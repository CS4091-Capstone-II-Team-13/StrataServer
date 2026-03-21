package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"strata.themrdt.org/dev/server/internal/model"
	"strata.themrdt.org/dev/server/internal/store/postgres"
)

var (
	ErrFileLocked   = errors.New("file is locked by another user")
	ErrLockNotFound = errors.New("lock not found")
	ErrNotLockOwner = errors.New("you do not own this lock")
)

type LockService struct {
	locks *postgres.LockStore
}

func NewLockService(locks *postgres.LockStore) *LockService {
	return &LockService{locks: locks}
}

// Acquire locks a file path for a user on a branch.
func (s *LockService) Acquire(ctx context.Context, projectID, filePath, branch, userID string) (*model.Lock, error) {
	// Check if already locked by someone else.
	existing, err := s.locks.GetByPath(ctx, projectID, filePath)
	if err != nil {
		return nil, fmt.Errorf("check existing lock: %w", err)
	}
	if existing != nil && existing.LockedBy != userID {
		return nil, fmt.Errorf("%w: locked by %s since %s",
			ErrFileLocked, existing.LockedByUsername, existing.LockedAt.Format(time.RFC3339))
	}
	if existing != nil && existing.LockedBy == userID {
		return existing, nil // Already locked by this user.
	}

	lock := &model.Lock{
		ID:        generateUUID(),
		ProjectID: projectID,
		FilePath:  filePath,
		Branch:    branch,
		LockedBy:  userID,
		LockedAt:  time.Now(),
	}

	if err := s.locks.Acquire(ctx, lock); err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}

	return lock, nil
}

// Release unlocks a file. Only the owner can release unless force is true.
func (s *LockService) Release(ctx context.Context, projectID, filePath, userID string, force bool) error {
	return s.locks.Release(ctx, projectID, filePath, userID, force)
}

// List returns all active locks for a project.
func (s *LockService) List(ctx context.Context, projectID string) ([]*model.Lock, error) {
	return s.locks.ListByProject(ctx, projectID)
}

// CheckPushPaths verifies that none of the given file paths are locked by
// someone other than the pushing user. Returns conflicting locks.
func (s *LockService) CheckPushPaths(ctx context.Context, projectID string, paths []string, userID string) ([]*model.Lock, error) {
	return s.locks.CheckPaths(ctx, projectID, paths, userID)
}
