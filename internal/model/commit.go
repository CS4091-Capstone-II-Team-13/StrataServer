package model

import "time"

type Commit struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	ParentID  *string   `json:"parent_id,omitempty"`  // Kept for backward compat.
	ParentIDs []string  `json:"parent_ids"`            // Source of truth.
	AuthorID  string    `json:"author_id"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// IsMergeCommit returns true if this commit has two or more parents.
func (c *Commit) IsMergeCommit() bool {
	return len(c.ParentIDs) >= 2
}

type Ref struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"` // "branch" or "tag"
	CommitID  string    `json:"commit_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CommitFile links a commit to a specific file version in its snapshot.
type CommitFile struct {
	CommitID      string `json:"commit_id"`
	FileID        string `json:"file_id"`
	FileVersionID string `json:"file_version_id"`
}

type Lock struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	FilePath  string    `json:"file_path"`
	Branch    string    `json:"branch"`
	LockedBy  string    `json:"locked_by"`
	LockedAt  time.Time `json:"locked_at"`
	// Populated by joins, not stored directly.
	LockedByUsername string `json:"locked_by_username,omitempty"`
}

// MergeType describes the outcome of a merge analysis.
type MergeType string

const (
	MergeTypeFastForward MergeType = "fast_forward"
	MergeTypeClean       MergeType = "clean"
	MergeTypeConflict    MergeType = "conflict"
)

// MergeAnalysis is the result of analyzing whether two branches can be merged.
type MergeAnalysis struct {
	Type         MergeType        `json:"type"`
	MergeBaseID  string           `json:"merge_base_id"`
	SourceBranch string           `json:"source_branch"`
	TargetBranch string           `json:"target_branch"`
	SourceOnly   []MergeFileEntry `json:"source_only,omitempty"`
	TargetOnly   []MergeFileEntry `json:"target_only,omitempty"`
	Conflicts    []MergeConflict  `json:"conflicts,omitempty"`
	Unchanged    int              `json:"unchanged"`
}

type MergeFileEntry struct {
	FileID        string `json:"file_id"`
	FilePath      string `json:"file_path"`
	FileVersionID string `json:"file_version_id"`
}

type MergeConflict struct {
	FileID              string `json:"file_id"`
	FilePath            string `json:"file_path"`
	BaseFileVersionID   string `json:"base_file_version_id,omitempty"`
	SourceFileVersionID string `json:"source_file_version_id"`
	TargetFileVersionID string `json:"target_file_version_id"`
}

// MergeResolution is a client's choice for a conflicted file.
type MergeResolution struct {
	FileID        string `json:"file_id"`
	FileVersionID string `json:"file_version_id"`
}
