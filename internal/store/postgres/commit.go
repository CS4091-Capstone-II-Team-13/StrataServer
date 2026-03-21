package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"strata.themrdt.org/dev/server/internal/model"
)

type CommitStore struct {
	pool *pgxpool.Pool
}

func NewCommitStore(pool *pgxpool.Pool) *CommitStore {
	return &CommitStore{pool: pool}
}

func (s *CommitStore) Create(ctx context.Context, c *model.Commit) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO commits (id, project_id, parent_id, parent_ids, author_id, message, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		c.ID, c.ProjectID, c.ParentID, c.ParentIDs, c.AuthorID, c.Message, c.CreatedAt,
	)
	return err
}

func (s *CommitStore) GetByID(ctx context.Context, id string) (*model.Commit, error) {
	c := &model.Commit{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, project_id, parent_id, parent_ids, author_id, message, created_at
		 FROM commits WHERE id = $1`,
		id,
	).Scan(&c.ID, &c.ProjectID, &c.ParentID, &c.ParentIDs, &c.AuthorID, &c.Message, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *CommitStore) ListByProject(ctx context.Context, projectID string, limit, offset int) ([]*model.Commit, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, project_id, parent_id, parent_ids, author_id, message, created_at
		 FROM commits
		 WHERE project_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		projectID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commits []*model.Commit
	for rows.Next() {
		c := &model.Commit{}
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.ParentID, &c.ParentIDs, &c.AuthorID, &c.Message, &c.CreatedAt); err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}
	return commits, rows.Err()
}

// ── Commit file snapshots ────────────────────────────────────────

// SetCommitFiles writes the full file tree snapshot for a commit.
func (s *CommitStore) SetCommitFiles(ctx context.Context, commitID string, entries []model.CommitFile) error {
	if len(entries) == 0 {
		return nil
	}

	// Use a batch insert via unnest for efficiency.
	commitIDs := make([]string, len(entries))
	fileIDs := make([]string, len(entries))
	versionIDs := make([]string, len(entries))
	for i, e := range entries {
		commitIDs[i] = commitID
		fileIDs[i] = e.FileID
		versionIDs[i] = e.FileVersionID
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO commit_files (commit_id, file_id, file_version_id)
		 SELECT * FROM unnest($1::uuid[], $2::uuid[], $3::uuid[])
		 ON CONFLICT (commit_id, file_id) DO UPDATE
		 SET file_version_id = EXCLUDED.file_version_id`,
		commitIDs, fileIDs, versionIDs,
	)
	return err
}

// GetCommitSnapshot returns the full file tree for a commit.
func (s *CommitStore) GetCommitSnapshot(ctx context.Context, commitID string) ([]model.CommitFile, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT commit_id, file_id, file_version_id
		 FROM commit_files
		 WHERE commit_id = $1`,
		commitID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []model.CommitFile
	for rows.Next() {
		var e model.CommitFile
		if err := rows.Scan(&e.CommitID, &e.FileID, &e.FileVersionID); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// GetCommitSnapshotWithPaths returns the snapshot with file paths resolved.
func (s *CommitStore) GetCommitSnapshotWithPaths(ctx context.Context, commitID string) ([]model.MergeFileEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT cf.file_id, f.path, cf.file_version_id
		 FROM commit_files cf
		 INNER JOIN files f ON f.id = cf.file_id
		 WHERE cf.commit_id = $1
		 ORDER BY f.path`,
		commitID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []model.MergeFileEntry
	for rows.Next() {
		var e model.MergeFileEntry
		if err := rows.Scan(&e.FileID, &e.FilePath, &e.FileVersionID); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// FindMergeBase finds the most recent common ancestor of two commits.
// Uses a BFS approach: walk both ancestor chains and return the first
// commit that appears in both.
func (s *CommitStore) FindMergeBase(ctx context.Context, commitA, commitB string) (string, error) {
	// Use a recursive CTE to find all ancestors of both commits,
	// then find the most recent common one.
	var baseID string
	err := s.pool.QueryRow(ctx,
		`WITH RECURSIVE
		ancestors_a AS (
			SELECT id, parent_ids, created_at FROM commits WHERE id = $1
			UNION
			SELECT c.id, c.parent_ids, c.created_at
			FROM commits c
			INNER JOIN ancestors_a a ON c.id = ANY(a.parent_ids)
		),
		ancestors_b AS (
			SELECT id, parent_ids, created_at FROM commits WHERE id = $2
			UNION
			SELECT c.id, c.parent_ids, c.created_at
			FROM commits c
			INNER JOIN ancestors_b b ON c.id = ANY(b.parent_ids)
		)
		SELECT a.id
		FROM ancestors_a a
		INNER JOIN ancestors_b b ON a.id = b.id
		ORDER BY a.created_at DESC
		LIMIT 1`,
		commitA, commitB,
	).Scan(&baseID)
	if err != nil {
		return "", err
	}
	return baseID, nil
}

// IsAncestor checks if ancestorID is an ancestor of descendantID.
func (s *CommitStore) IsAncestor(ctx context.Context, ancestorID, descendantID string) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx,
		`WITH RECURSIVE walk AS (
			SELECT id, parent_ids FROM commits WHERE id = $2
			UNION
			SELECT c.id, c.parent_ids
			FROM commits c
			INNER JOIN walk w ON c.id = ANY(w.parent_ids)
		)
		SELECT EXISTS(SELECT 1 FROM walk WHERE id = $1)`,
		ancestorID, descendantID,
	).Scan(&found)
	return found, err
}

// ── Refs ─────────────────────────────────────────────────────────

func (s *CommitStore) GetRef(ctx context.Context, projectID, name, refType string) (*model.Ref, error) {
	ref := &model.Ref{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, project_id, name, type, commit_id, updated_at
		 FROM refs
		 WHERE project_id = $1 AND name = $2 AND type = $3`,
		projectID, name, refType,
	).Scan(&ref.ID, &ref.ProjectID, &ref.Name, &ref.Type, &ref.CommitID, &ref.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return ref, nil
}

func (s *CommitStore) UpsertRef(ctx context.Context, ref *model.Ref) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO refs (id, project_id, name, type, commit_id, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (project_id, name, type) DO UPDATE
		 SET commit_id = EXCLUDED.commit_id, updated_at = EXCLUDED.updated_at`,
		ref.ID, ref.ProjectID, ref.Name, ref.Type, ref.CommitID, ref.UpdatedAt,
	)
	return err
}

func (s *CommitStore) ListRefs(ctx context.Context, projectID string) ([]*model.Ref, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, project_id, name, type, commit_id, updated_at
		 FROM refs
		 WHERE project_id = $1
		 ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*model.Ref
	for rows.Next() {
		ref := &model.Ref{}
		if err := rows.Scan(&ref.ID, &ref.ProjectID, &ref.Name, &ref.Type, &ref.CommitID, &ref.UpdatedAt); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}
