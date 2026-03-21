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
	ErrBranchNotFound      = errors.New("branch not found")
	ErrSameBranch          = errors.New("cannot merge a branch into itself")
	ErrNoMergeBase         = errors.New("branches have no common ancestor")
	ErrUnresolvedConflicts = errors.New("merge has unresolved conflicts")
	ErrInvalidResolution   = errors.New("resolution references invalid file version")
)

type MergeService struct {
	commits *postgres.CommitStore
	files   *postgres.FileStore
	chunks  *postgres.ChunkStore
}

func NewMergeService(commits *postgres.CommitStore, files *postgres.FileStore, chunks *postgres.ChunkStore) *MergeService {
	return &MergeService{
		commits: commits,
		files:   files,
		chunks:  chunks,
	}
}

// Analyze examines two branches and determines the merge type.
// This does NOT create any commits — it returns what would happen.
func (s *MergeService) Analyze(ctx context.Context, projectID, sourceBranch, targetBranch string) (*model.MergeAnalysis, error) {
	if sourceBranch == targetBranch {
		return nil, ErrSameBranch
	}

	// Resolve branch refs to commit IDs.
	sourceRef, err := s.commits.GetRef(ctx, projectID, sourceBranch, "branch")
	if err != nil {
		return nil, fmt.Errorf("source branch: %w", ErrBranchNotFound)
	}
	targetRef, err := s.commits.GetRef(ctx, projectID, targetBranch, "branch")
	if err != nil {
		return nil, fmt.Errorf("target branch: %w", ErrBranchNotFound)
	}

	// Check for fast-forward: is target head an ancestor of source head?
	isAncestor, err := s.commits.IsAncestor(ctx, targetRef.CommitID, sourceRef.CommitID)
	if err != nil {
		return nil, fmt.Errorf("check ancestry: %w", err)
	}
	if isAncestor {
		return &model.MergeAnalysis{
			Type:         model.MergeTypeFastForward,
			MergeBaseID:  targetRef.CommitID,
			SourceBranch: sourceBranch,
			TargetBranch: targetBranch,
		}, nil
	}

	// Find the merge base (most recent common ancestor).
	baseID, err := s.commits.FindMergeBase(ctx, sourceRef.CommitID, targetRef.CommitID)
	if err != nil {
		return nil, ErrNoMergeBase
	}

	// Get the file snapshots at all three points.
	baseSnapshot, err := s.commits.GetCommitSnapshotWithPaths(ctx, baseID)
	if err != nil {
		return nil, fmt.Errorf("get base snapshot: %w", err)
	}
	sourceSnapshot, err := s.commits.GetCommitSnapshotWithPaths(ctx, sourceRef.CommitID)
	if err != nil {
		return nil, fmt.Errorf("get source snapshot: %w", err)
	}
	targetSnapshot, err := s.commits.GetCommitSnapshotWithPaths(ctx, targetRef.CommitID)
	if err != nil {
		return nil, fmt.Errorf("get target snapshot: %w", err)
	}

	// Build maps: fileID → MergeFileEntry for each snapshot.
	baseMap := snapshotMap(baseSnapshot)
	sourceMap := snapshotMap(sourceSnapshot)
	targetMap := snapshotMap(targetSnapshot)

	analysis := &model.MergeAnalysis{
		Type:         model.MergeTypeClean,
		MergeBaseID:  baseID,
		SourceBranch: sourceBranch,
		TargetBranch: targetBranch,
	}

	// Collect all file IDs across all three snapshots.
	allFileIDs := make(map[string]bool)
	for id := range baseMap {
		allFileIDs[id] = true
	}
	for id := range sourceMap {
		allFileIDs[id] = true
	}
	for id := range targetMap {
		allFileIDs[id] = true
	}

	for fileID := range allFileIDs {
		baseEntry, inBase := baseMap[fileID]
		sourceEntry, inSource := sourceMap[fileID]
		targetEntry, inTarget := targetMap[fileID]

		baseVersion := ""
		if inBase {
			baseVersion = baseEntry.FileVersionID
		}
		sourceVersion := ""
		if inSource {
			sourceVersion = sourceEntry.FileVersionID
		}
		targetVersion := ""
		if inTarget {
			targetVersion = targetEntry.FileVersionID
		}

		sourceChanged := sourceVersion != baseVersion
		targetChanged := targetVersion != baseVersion

		switch {
		case !sourceChanged && !targetChanged:
			// Unchanged on both sides.
			analysis.Unchanged++

		case sourceChanged && !targetChanged:
			// Only source branch modified this file.
			if inSource {
				analysis.SourceOnly = append(analysis.SourceOnly, sourceEntry)
			}
			// If source deleted (not in source but was in base), we skip it
			// from the merged snapshot (deletion propagates).

		case !sourceChanged && targetChanged:
			// Only target branch modified this file.
			if inTarget {
				analysis.TargetOnly = append(analysis.TargetOnly, targetEntry)
			}

		case sourceChanged && targetChanged:
			if sourceVersion == targetVersion {
				// Both made the same change — no conflict.
				analysis.Unchanged++
			} else {
				// Both changed differently — conflict.
				analysis.Type = model.MergeTypeConflict

				filePath := ""
				if inSource {
					filePath = sourceEntry.FilePath
				} else if inTarget {
					filePath = targetEntry.FilePath
				} else if inBase {
					filePath = baseEntry.FilePath
				}

				analysis.Conflicts = append(analysis.Conflicts, model.MergeConflict{
					FileID:              fileID,
					FilePath:            filePath,
					BaseFileVersionID:   baseVersion,
					SourceFileVersionID: sourceVersion,
					TargetFileVersionID: targetVersion,
				})
			}
		}
	}

	return analysis, nil
}

// Execute performs the merge. For fast-forward, just moves the ref. For clean
// and resolved merges, creates a merge commit with two parents and a combined
// snapshot.
func (s *MergeService) Execute(ctx context.Context, projectID, sourceBranch, targetBranch, authorID, message string, resolutions []model.MergeResolution) (*model.Commit, error) {
	// Re-analyze to get current state (prevents TOCTOU issues).
	analysis, err := s.Analyze(ctx, projectID, sourceBranch, targetBranch)
	if err != nil {
		return nil, err
	}

	sourceRef, _ := s.commits.GetRef(ctx, projectID, sourceBranch, "branch")
	targetRef, _ := s.commits.GetRef(ctx, projectID, targetBranch, "branch")

	// Handle fast-forward.
	if analysis.Type == model.MergeTypeFastForward {
		now := time.Now()
		targetRef.CommitID = sourceRef.CommitID
		targetRef.UpdatedAt = now
		if err := s.commits.UpsertRef(ctx, targetRef); err != nil {
			return nil, fmt.Errorf("fast-forward ref: %w", err)
		}
		// Return the source commit as the result (no new commit created).
		return s.commits.GetByID(ctx, sourceRef.CommitID)
	}

	// For clean or conflict merges, validate resolutions cover all conflicts.
	if analysis.Type == model.MergeTypeConflict {
		if len(resolutions) != len(analysis.Conflicts) {
			return nil, ErrUnresolvedConflicts
		}
		// Validate each resolution.
		resolutionMap := make(map[string]string)
		for _, r := range resolutions {
			resolutionMap[r.FileID] = r.FileVersionID
		}
		for _, c := range analysis.Conflicts {
			versionID, ok := resolutionMap[c.FileID]
			if !ok {
				return nil, fmt.Errorf("%w: missing resolution for file %s", ErrUnresolvedConflicts, c.FilePath)
			}
			// Verify the chosen version is one of the valid options.
			if versionID != c.SourceFileVersionID && versionID != c.TargetFileVersionID && versionID != c.BaseFileVersionID {
				return nil, fmt.Errorf("%w: file %s", ErrInvalidResolution, c.FilePath)
			}
		}
	}

	// Build the merged file tree snapshot.
	//
	// Start with the target's current snapshot (since we're merging into target).
	targetSnapshot, err := s.commits.GetCommitSnapshotWithPaths(ctx, targetRef.CommitID)
	if err != nil {
		return nil, fmt.Errorf("get target snapshot: %w", err)
	}
	mergedMap := make(map[string]model.CommitFile)
	for _, entry := range targetSnapshot {
		mergedMap[entry.FileID] = model.CommitFile{
			FileID:        entry.FileID,
			FileVersionID: entry.FileVersionID,
		}
	}

	// Apply source-only changes (take source version).
	for _, entry := range analysis.SourceOnly {
		mergedMap[entry.FileID] = model.CommitFile{
			FileID:        entry.FileID,
			FileVersionID: entry.FileVersionID,
		}
	}

	// Apply conflict resolutions.
	if analysis.Type == model.MergeTypeConflict {
		for _, r := range resolutions {
			mergedMap[r.FileID] = model.CommitFile{
				FileID:        r.FileID,
				FileVersionID: r.FileVersionID,
			}
		}
	}

	// Create the merge commit.
	now := time.Now()
	if message == "" {
		message = fmt.Sprintf("Merge %s into %s", sourceBranch, targetBranch)
	}

	commitID := generateUUID()
	commit := &model.Commit{
		ID:        commitID,
		ProjectID: projectID,
		ParentIDs: []string{targetRef.CommitID, sourceRef.CommitID},
		AuthorID:  authorID,
		Message:   message,
		CreatedAt: now,
	}

	// Set parent_id to target for backward compat.
	commit.ParentID = &targetRef.CommitID

	if err := s.commits.Create(ctx, commit); err != nil {
		return nil, fmt.Errorf("create merge commit: %w", err)
	}

	// Write the merged snapshot.
	var snapshot []model.CommitFile
	for _, cf := range mergedMap {
		cf.CommitID = commitID
		snapshot = append(snapshot, cf)
	}
	if err := s.commits.SetCommitFiles(ctx, commitID, snapshot); err != nil {
		return nil, fmt.Errorf("write merge snapshot: %w", err)
	}

	// Advance the target branch ref.
	targetRef.CommitID = commitID
	targetRef.UpdatedAt = now
	if err := s.commits.UpsertRef(ctx, targetRef); err != nil {
		return nil, fmt.Errorf("advance target ref: %w", err)
	}

	return commit, nil
}

func snapshotMap(entries []model.MergeFileEntry) map[string]model.MergeFileEntry {
	m := make(map[string]model.MergeFileEntry, len(entries))
	for _, e := range entries {
		m[e.FileID] = e
	}
	return m
}
