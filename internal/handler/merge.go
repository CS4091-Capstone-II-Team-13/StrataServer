package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"strata.themrdt.org/dev/server/internal/middleware"
	"strata.themrdt.org/dev/server/internal/model"
	"strata.themrdt.org/dev/server/internal/service"
	"strata.themrdt.org/dev/server/internal/store/postgres"
)

type MergeHandler struct {
	merges  *service.MergeService
	commits *postgres.CommitStore
}

func NewMergeHandler(merges *service.MergeService, commits *postgres.CommitStore) *MergeHandler {
	return &MergeHandler{merges: merges, commits: commits}
}

// CreateBranch creates a new branch pointing at an existing commit.
func (h *MergeHandler) CreateBranch(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	var req struct {
		Name    string `json:"name"`
		FromRef string `json:"from_ref"` // Branch or tag name to branch from.
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "name is required"))
		return
	}
	if req.FromRef == "" {
		req.FromRef = "main"
	}

	// Resolve the source ref.
	sourceRef, err := h.commits.GetRef(r.Context(), projectID, req.FromRef, "branch")
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("REF_NOT_FOUND", "Source branch not found"))
		return
	}

	// Check if branch already exists.
	_, err = h.commits.GetRef(r.Context(), projectID, req.Name, "branch")
	if err == nil {
		writeJSON(w, http.StatusConflict, errorBody("BRANCH_EXISTS", "Branch already exists"))
		return
	}

	// Create the new ref.
	now := time.Now()
	ref := &model.Ref{
		ID:        generateBranchUUID(),
		ProjectID: projectID,
		Name:      req.Name,
		Type:      "branch",
		CommitID:  sourceRef.CommitID,
		UpdatedAt: now,
	}

	if err := h.commits.UpsertRef(r.Context(), ref); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to create branch"))
		return
	}

	writeJSON(w, http.StatusCreated, ref)
}

// AnalyzeMerge returns what would happen if source were merged into target.
func (h *MergeHandler) AnalyzeMerge(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	var req struct {
		SourceBranch string `json:"source_branch"`
		TargetBranch string `json:"target_branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	if req.SourceBranch == "" || req.TargetBranch == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "source_branch and target_branch are required"))
		return
	}

	analysis, err := h.merges.Analyze(r.Context(), projectID, req.SourceBranch, req.TargetBranch)
	if err != nil {
		if errors.Is(err, service.ErrSameBranch) {
			writeJSON(w, http.StatusBadRequest, errorBody("SAME_BRANCH", "Cannot merge a branch into itself"))
			return
		}
		if errors.Is(err, service.ErrNoMergeBase) {
			writeJSON(w, http.StatusConflict, errorBody("NO_MERGE_BASE", "Branches have no common ancestor"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to analyze merge"))
		return
	}

	writeJSON(w, http.StatusOK, analysis)
}

// ExecuteMerge performs the merge, optionally with conflict resolutions.
func (h *MergeHandler) ExecuteMerge(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := middleware.UserIDFromContext(r.Context())

	var req struct {
		SourceBranch string                  `json:"source_branch"`
		TargetBranch string                  `json:"target_branch"`
		Message      string                  `json:"message"`
		Resolutions  []model.MergeResolution `json:"resolutions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	if req.SourceBranch == "" || req.TargetBranch == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "source_branch and target_branch are required"))
		return
	}

	commit, err := h.merges.Execute(r.Context(), projectID, req.SourceBranch, req.TargetBranch, userID, req.Message, req.Resolutions)
	if err != nil {
		if errors.Is(err, service.ErrUnresolvedConflicts) {
			writeJSON(w, http.StatusConflict, errorBody("UNRESOLVED_CONFLICTS", "Merge has unresolved conflicts — analyze first and provide resolutions"))
			return
		}
		if errors.Is(err, service.ErrInvalidResolution) {
			writeJSON(w, http.StatusBadRequest, errorBody("INVALID_RESOLUTION", err.Error()))
			return
		}
		if errors.Is(err, service.ErrSameBranch) {
			writeJSON(w, http.StatusBadRequest, errorBody("SAME_BRANCH", "Cannot merge a branch into itself"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to execute merge"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"commit":  commit,
		"message": "Merge successful",
	})
}

// generateBranchUUID is a helper — reuses the auth service's UUID gen.
func generateBranchUUID() string {
	return service.GenerateUUID()
}
