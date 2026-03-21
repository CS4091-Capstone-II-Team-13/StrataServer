package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"strata.themrdt.org/dev/server/internal/service"
)

type VersionHandler struct {
	versions *service.VersionService
}

func NewVersionHandler(versions *service.VersionService) *VersionHandler {
	return &VersionHandler{versions: versions}
}

func (h *VersionHandler) ListCommits(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	commits, err := h.versions.ListCommits(r.Context(), projectID, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to list commits"))
		return
	}

	writeJSON(w, http.StatusOK, commits)
}

func (h *VersionHandler) GetCommit(w http.ResponseWriter, r *http.Request) {
	commitID := chi.URLParam(r, "commitID")

	commit, err := h.versions.GetCommit(r.Context(), commitID)
	if err != nil {
		if errors.Is(err, service.ErrCommitNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("COMMIT_NOT_FOUND", "Commit not found"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to get commit"))
		return
	}

	writeJSON(w, http.StatusOK, commit)
}

func (h *VersionHandler) ListCommitFiles(w http.ResponseWriter, r *http.Request) {
	commitID := chi.URLParam(r, "commitID")

	files, err := h.versions.GetCommitFiles(r.Context(), commitID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to list commit files"))
		return
	}

	writeJSON(w, http.StatusOK, files)
}

func (h *VersionHandler) ListRefs(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	refs, err := h.versions.ListRefs(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to list refs"))
		return
	}

	writeJSON(w, http.StatusOK, refs)
}

func (h *VersionHandler) GetTree(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	refName := r.URL.Query().Get("ref")

	if refName == "" {
		refName = "main"
	}

	// Resolve ref to commit.
	ref, err := h.versions.ResolveRef(r.Context(), projectID, refName)
	if err != nil {
		if errors.Is(err, service.ErrRefNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("REF_NOT_FOUND", fmt.Sprintf("Ref %q not found", refName)))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to resolve ref"))
		return
	}

	files, err := h.versions.GetFileTree(r.Context(), projectID, ref.CommitID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to get file tree"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ref":       refName,
		"commit_id": ref.CommitID,
		"files":     files,
	})
}
