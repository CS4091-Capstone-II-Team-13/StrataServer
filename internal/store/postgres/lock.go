package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"strata.themrdt.org/dev/server/internal/model"
)

type LockStore struct {
	pool *pgxpool.Pool
}

func NewLockStore(pool *pgxpool.Pool) *LockStore {
	return &LockStore{pool: pool}
}

// Acquire attempts to lock a file path. Returns an error if already locked
// by a different user.
func (s *LockStore) Acquire(ctx context.Context, lock *model.Lock) error {
	// Check if already locked.
	existing, err := s.GetByPath(ctx, lock.ProjectID, lock.FilePath)
	if err == nil && existing != nil {
		if existing.LockedBy == lock.LockedBy {
			// Already locked by the same user — idempotent success.
			return nil
		}
		return errors.New("file is locked by another user")
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO locks (id, project_id, file_path, branch, locked_by, locked_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (project_id, file_path) DO NOTHING`,
		lock.ID, lock.ProjectID, lock.FilePath, lock.Branch, lock.LockedBy, lock.LockedAt,
	)
	return err
}

// Release removes a lock. Only the lock owner or a force-unlock can release.
func (s *LockStore) Release(ctx context.Context, projectID, filePath, userID string, force bool) error {
	if force {
		_, err := s.pool.Exec(ctx,
			`DELETE FROM locks WHERE project_id = $1 AND file_path = $2`,
			projectID, filePath,
		)
		return err
	}

	result, err := s.pool.Exec(ctx,
		`DELETE FROM locks WHERE project_id = $1 AND file_path = $2 AND locked_by = $3`,
		projectID, filePath, userID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return errors.New("lock not found or owned by another user")
	}
	return nil
}

// GetByPath returns the lock for a specific file, or nil if not locked.
func (s *LockStore) GetByPath(ctx context.Context, projectID, filePath string) (*model.Lock, error) {
	lock := &model.Lock{}
	err := s.pool.QueryRow(ctx,
		`SELECT l.id, l.project_id, l.file_path, l.branch, l.locked_by, l.locked_at, u.username
		 FROM locks l
		 INNER JOIN users u ON u.id = l.locked_by
		 WHERE l.project_id = $1 AND l.file_path = $2`,
		projectID, filePath,
	).Scan(&lock.ID, &lock.ProjectID, &lock.FilePath, &lock.Branch, &lock.LockedBy, &lock.LockedAt, &lock.LockedByUsername)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return lock, nil
}

// ListByProject returns all active locks for a project.
func (s *LockStore) ListByProject(ctx context.Context, projectID string) ([]*model.Lock, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT l.id, l.project_id, l.file_path, l.branch, l.locked_by, l.locked_at, u.username
		 FROM locks l
		 INNER JOIN users u ON u.id = l.locked_by
		 WHERE l.project_id = $1
		 ORDER BY l.file_path`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locks []*model.Lock
	for rows.Next() {
		lock := &model.Lock{}
		if err := rows.Scan(&lock.ID, &lock.ProjectID, &lock.FilePath, &lock.Branch, &lock.LockedBy, &lock.LockedAt, &lock.LockedByUsername); err != nil {
			return nil, err
		}
		locks = append(locks, lock)
	}
	return locks, rows.Err()
}

// CheckPaths checks which of the given paths are locked by someone other than
// the specified user. Returns the conflicting locks.
func (s *LockStore) CheckPaths(ctx context.Context, projectID string, paths []string, userID string) ([]*model.Lock, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT l.id, l.project_id, l.file_path, l.branch, l.locked_by, l.locked_at, u.username
		 FROM locks l
		 INNER JOIN users u ON u.id = l.locked_by
		 WHERE l.project_id = $1
		   AND l.file_path = ANY($2)
		   AND l.locked_by != $3`,
		projectID, paths, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locks []*model.Lock
	for rows.Next() {
		lock := &model.Lock{}
		if err := rows.Scan(&lock.ID, &lock.ProjectID, &lock.FilePath, &lock.Branch, &lock.LockedBy, &lock.LockedAt, &lock.LockedByUsername); err != nil {
			return nil, err
		}
		locks = append(locks, lock)
	}
	return locks, rows.Err()
}
