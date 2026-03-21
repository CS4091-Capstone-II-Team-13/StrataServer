package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"strata.themrdt.org/dev/server/internal/model"
)

type FileStore struct {
	pool *pgxpool.Pool
}

func NewFileStore(pool *pgxpool.Pool) *FileStore {
	return &FileStore{pool: pool}
}

func (s *FileStore) Upsert(ctx context.Context, f *model.File) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO files (id, project_id, path, created_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (project_id, path) DO UPDATE
		 SET id = files.id`, // no-op update to return the existing row
		f.ID, f.ProjectID, f.Path, f.CreatedAt,
	)
	return err
}

func (s *FileStore) GetByPath(ctx context.Context, projectID, path string) (*model.File, error) {
	f := &model.File{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, project_id, path, created_at
		 FROM files WHERE project_id = $1 AND path = $2`,
		projectID, path,
	).Scan(&f.ID, &f.ProjectID, &f.Path, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *FileStore) CreateVersion(ctx context.Context, fv *model.FileVersion) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO file_versions (id, file_id, commit_id, total_size, chunk_count, chunk_hashes, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		fv.ID, fv.FileID, fv.CommitID, fv.TotalSize, fv.ChunkCount, fv.ChunkHashes, fv.CreatedAt,
	)
	return err
}

func (s *FileStore) GetVersionsByCommit(ctx context.Context, commitID string) ([]*model.FileVersion, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT fv.id, fv.file_id, fv.commit_id, fv.total_size, fv.chunk_count, fv.chunk_hashes, fv.created_at
		 FROM file_versions fv
		 WHERE fv.commit_id = $1`,
		commitID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var versions []*model.FileVersion
	for rows.Next() {
		fv := &model.FileVersion{}
		if err := rows.Scan(&fv.ID, &fv.FileID, &fv.CommitID, &fv.TotalSize, &fv.ChunkCount, &fv.ChunkHashes, &fv.CreatedAt); err != nil {
			return nil, err
		}
		versions = append(versions, fv)
	}
	return versions, rows.Err()
}

// ListFileTree returns all files in a project with their latest version info at a given commit.
func (s *FileStore) ListFileTree(ctx context.Context, projectID, commitID string) ([]*model.File, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT f.id, f.project_id, f.path, f.created_at
		 FROM files f
		 INNER JOIN file_versions fv ON fv.file_id = f.id
		 WHERE f.project_id = $1 AND fv.commit_id = $2
		 ORDER BY f.path`,
		projectID, commitID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*model.File
	for rows.Next() {
		f := &model.File{}
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.Path, &f.CreatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}
