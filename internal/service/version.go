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
	ErrCommitConflict = errors.New("parent commit does not match current ref head")
	ErrCommitNotFound = errors.New("commit not found")
	ErrRefNotFound    = errors.New("ref not found")
)

type VersionService struct {
	commits *postgres.CommitStore
	files   *postgres.FileStore
	chunks  *postgres.ChunkStore
}

func NewVersionService(commits *postgres.CommitStore, files *postgres.FileStore, chunks *postgres.ChunkStore) *VersionService {
	return &VersionService{
		commits: commits,
		files:   files,
		chunks:  chunks,
	}
}

// CommitRequest represents a push/commit request from the client.
type CommitRequest struct {
	ProjectID      string
	ParentCommitID *string
	AuthorID       string
	Message        string
	Ref            string
	Files          []CommitFileEntry
}

type CommitFileEntry struct {
	Path        string   `json:"path"`
	TotalSize   int64    `json:"total_size"`
	ChunkHashes []string `json:"chunk_hashes"`
}

// CommitResult is returned after a successful commit.
type CommitResult struct {
	CommitID       string `json:"commit_id"`
	Ref            string `json:"ref"`
	FilesUpdated   int    `json:"files_updated"`
	TotalNewChunks int    `json:"total_new_chunks"`
}

// CreateCommit validates the parent, creates a commit with all file versions,
// increments chunk ref counts, and advances the ref pointer. This should be
// called within a transaction in production; for v1 the individual store calls
// are sequential.
func (s *VersionService) CreateCommit(ctx context.Context, req *CommitRequest) (*CommitResult, error) {
	// Verify parent matches current ref head (optimistic concurrency).
	if req.Ref != "" {
		ref, err := s.commits.GetRef(ctx, req.ProjectID, req.Ref, "branch")
		if err == nil {
			// Ref exists — parent must match.
			if req.ParentCommitID == nil || *req.ParentCommitID != ref.CommitID {
				return nil, ErrCommitConflict
			}
		}
		// If ref doesn't exist yet (first commit), parent should be nil.
		if err != nil && req.ParentCommitID != nil {
			return nil, ErrCommitConflict
		}
	}

	now := time.Now()
	commitID := generateUUID()

	// Build parent_ids array (must be non-nil for PostgreSQL NOT NULL).
	parentIDs := []string{}
	if req.ParentCommitID != nil {
		parentIDs = []string{*req.ParentCommitID}
	}

	// Create the commit record.
	commit := &model.Commit{
		ID:        commitID,
		ProjectID: req.ProjectID,
		ParentID:  req.ParentCommitID,
		ParentIDs: parentIDs,
		AuthorID:  req.AuthorID,
		Message:   req.Message,
		CreatedAt: now,
	}
	if err := s.commits.Create(ctx, commit); err != nil {
		return nil, fmt.Errorf("create commit: %w", err)
	}

	// Start building the commit snapshot from the parent's snapshot.
	snapshotMap := make(map[string]model.CommitFile) // fileID → CommitFile
	if req.ParentCommitID != nil {
		parentSnapshot, err := s.commits.GetCommitSnapshot(ctx, *req.ParentCommitID)
		if err == nil {
			for _, cf := range parentSnapshot {
				snapshotMap[cf.FileID] = cf
			}
		}
		// If parent has no snapshot (pre-migration commits), that's OK — start fresh.
	}

	// Create file versions and collect all chunk hashes for ref counting.
	var allChunkHashes []string

	for _, f := range req.Files {
		// Upsert the file record.
		file := &model.File{
			ID:        generateUUID(),
			ProjectID: req.ProjectID,
			Path:      f.Path,
			CreatedAt: now,
		}
		if err := s.files.Upsert(ctx, file); err != nil {
			return nil, fmt.Errorf("upsert file %q: %w", f.Path, err)
		}

		// Look up the actual file ID (upsert may have used existing).
		existing, err := s.files.GetByPath(ctx, req.ProjectID, f.Path)
		if err != nil {
			return nil, fmt.Errorf("get file %q: %w", f.Path, err)
		}

		// Create the file version.
		fv := &model.FileVersion{
			ID:          generateUUID(),
			FileID:      existing.ID,
			CommitID:    commitID,
			TotalSize:   f.TotalSize,
			ChunkCount:  len(f.ChunkHashes),
			ChunkHashes: f.ChunkHashes,
			CreatedAt:   now,
		}
		if err := s.files.CreateVersion(ctx, fv); err != nil {
			return nil, fmt.Errorf("create file version for %q: %w", f.Path, err)
		}

		// Update the snapshot with this file's new version.
		snapshotMap[existing.ID] = model.CommitFile{
			CommitID:      commitID,
			FileID:        existing.ID,
			FileVersionID: fv.ID,
		}

		allChunkHashes = append(allChunkHashes, f.ChunkHashes...)
	}

	// Write the full commit snapshot.
	var snapshot []model.CommitFile
	for _, cf := range snapshotMap {
		cf.CommitID = commitID
		snapshot = append(snapshot, cf)
	}
	if err := s.commits.SetCommitFiles(ctx, commitID, snapshot); err != nil {
		return nil, fmt.Errorf("write commit snapshot: %w", err)
	}

	// Increment ref counts for all referenced chunks.
	if len(allChunkHashes) > 0 {
		if err := s.chunks.IncrementRefCount(ctx, allChunkHashes); err != nil {
			return nil, fmt.Errorf("increment chunk ref counts: %w", err)
		}
	}

	// Advance the ref pointer.
	if req.Ref != "" {
		ref := &model.Ref{
			ID:        generateUUID(),
			ProjectID: req.ProjectID,
			Name:      req.Ref,
			Type:      "branch",
			CommitID:  commitID,
			UpdatedAt: now,
		}
		if err := s.commits.UpsertRef(ctx, ref); err != nil {
			return nil, fmt.Errorf("update ref %q: %w", req.Ref, err)
		}
	}

	return &CommitResult{
		CommitID:     commitID,
		Ref:          req.Ref,
		FilesUpdated: len(req.Files),
	}, nil
}

// DiffRequest represents a pull/diff request from the client.
type DiffRequest struct {
	ProjectID    string
	FromCommitID *string // nil for fresh clone
	ToCommitID   string
}

// DiffResult describes what changed between two commits.
type DiffResult struct {
	ChangedFiles      []DiffFileEntry `json:"changed_files"`
	TotalDownloadSize int64           `json:"total_download_size"`
}

type DiffFileEntry struct {
	Path         string   `json:"path"`
	Action       string   `json:"action"` // "added", "modified", "deleted"
	NeededChunks []string `json:"needed_chunks"`
	FullManifest []string `json:"full_manifest"`
}

// ComputeDiff compares file versions between two commits and returns the
// chunks the client needs to download.
func (s *VersionService) ComputeDiff(ctx context.Context, req *DiffRequest) (*DiffResult, error) {
	// Get file versions at the target commit.
	toVersions, err := s.files.GetVersionsByCommit(ctx, req.ToCommitID)
	if err != nil {
		return nil, fmt.Errorf("get target versions: %w", err)
	}

	// Build a set of chunk hashes the client already has (from the source commit).
	haveChunks := make(map[string]struct{})
	if req.FromCommitID != nil {
		fromVersions, err := s.files.GetVersionsByCommit(ctx, *req.FromCommitID)
		if err != nil {
			return nil, fmt.Errorf("get source versions: %w", err)
		}
		for _, fv := range fromVersions {
			for _, h := range fv.ChunkHashes {
				haveChunks[h] = struct{}{}
			}
		}
	}

	result := &DiffResult{}

	for _, fv := range toVersions {
		entry := DiffFileEntry{
			Path:         fv.FileID, // TODO: resolve file path from file ID
			FullManifest: fv.ChunkHashes,
		}

		// Determine which chunks are new.
		for _, h := range fv.ChunkHashes {
			if _, have := haveChunks[h]; !have {
				entry.NeededChunks = append(entry.NeededChunks, h)
			}
		}

		if req.FromCommitID == nil {
			entry.Action = "added"
		} else if len(entry.NeededChunks) > 0 {
			entry.Action = "modified"
		} else {
			continue // No changes for this file.
		}

		result.ChangedFiles = append(result.ChangedFiles, entry)
	}

	return result, nil
}

// GetCommit retrieves a single commit by ID.
func (s *VersionService) GetCommit(ctx context.Context, id string) (*model.Commit, error) {
	c, err := s.commits.GetByID(ctx, id)
	if err != nil {
		return nil, ErrCommitNotFound
	}
	return c, nil
}

// ListCommits returns paginated commits for a project.
func (s *VersionService) ListCommits(ctx context.Context, projectID string, limit, offset int) ([]*model.Commit, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	return s.commits.ListByProject(ctx, projectID, limit, offset)
}

// ListRefs returns all refs (branches/tags) for a project.
func (s *VersionService) ListRefs(ctx context.Context, projectID string) ([]*model.Ref, error) {
	return s.commits.ListRefs(ctx, projectID)
}

// GetFileTree returns all files at a given commit.
func (s *VersionService) GetFileTree(ctx context.Context, projectID, commitID string) ([]*model.File, error) {
	return s.files.ListFileTree(ctx, projectID, commitID)
}

// GetCommitFiles returns all file versions for a commit.
func (s *VersionService) GetCommitFiles(ctx context.Context, commitID string) ([]*model.FileVersion, error) {
	return s.files.GetVersionsByCommit(ctx, commitID)
}

// ResolveRef looks up the commit ID for a branch or tag name.
func (s *VersionService) ResolveRef(ctx context.Context, projectID, refName string) (*model.Ref, error) {
	ref, err := s.commits.GetRef(ctx, projectID, refName, "branch")
	if err != nil {
		// Try tag.
		ref, err = s.commits.GetRef(ctx, projectID, refName, "tag")
		if err != nil {
			return nil, ErrRefNotFound
		}
	}
	return ref, nil
}
