package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"strata.themrdt.org/dev/server/internal/model"
)

type ChunkStore struct {
	pool *pgxpool.Pool
}

func NewChunkStore(pool *pgxpool.Pool) *ChunkStore {
	return &ChunkStore{pool: pool}
}

func (s *ChunkStore) Insert(ctx context.Context, chunk *model.Chunk) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO chunks (hash, size_bytes, minio_bucket, minio_key, ref_count, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (hash) DO NOTHING`,
		chunk.Hash, chunk.SizeBytes, chunk.MinioBucket, chunk.MinioKey, chunk.RefCount, chunk.CreatedAt,
	)
	return err
}

func (s *ChunkStore) GetByHash(ctx context.Context, hash string) (*model.Chunk, error) {
	c := &model.Chunk{}
	err := s.pool.QueryRow(ctx,
		`SELECT hash, size_bytes, minio_bucket, minio_key, ref_count, created_at
		 FROM chunks WHERE hash = $1`,
		hash,
	).Scan(&c.Hash, &c.SizeBytes, &c.MinioBucket, &c.MinioKey, &c.RefCount, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// CheckExisting takes a list of hashes and returns the subset that already
// exist in the store. Uses unnest + join for efficient bulk lookup.
func (s *ChunkStore) CheckExisting(ctx context.Context, hashes []string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT c.hash
		 FROM chunks c
		 INNER JOIN unnest($1::text[]) AS h(hash) ON c.hash = h.hash`,
		hashes,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var existing []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		existing = append(existing, h)
	}
	return existing, rows.Err()
}

// IncrementRefCount increments ref_count for the given hashes.
func (s *ChunkStore) IncrementRefCount(ctx context.Context, hashes []string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chunks SET ref_count = ref_count + 1
		 WHERE hash = ANY($1)`,
		hashes,
	)
	return err
}

// DecrementRefCount decrements ref_count for the given hashes.
func (s *ChunkStore) DecrementRefCount(ctx context.Context, hashes []string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chunks SET ref_count = ref_count - 1
		 WHERE hash = ANY($1)`,
		hashes,
	)
	return err
}
