package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"strata.themrdt.org/dev/server/internal/middleware"
	"strata.themrdt.org/dev/server/internal/service"
)

type LockHandler struct {
	locks *service.LockService
}

func NewLockHandler(locks *service.LockService) *LockHandler {
	return &LockHandler{locks: locks}
}

// Acquire locks a file path.
func (h *LockHandler) Acquire(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := middleware.UserIDFromContext(r.Context())

	var req struct {
		FilePath string `json:"file_path"`
		Branch   string `json:"branch"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	if req.FilePath == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "file_path is required"))
		return
	}
	if req.Branch == "" {
		req.Branch = "main"
	}

	lock, err := h.locks.Acquire(r.Context(), projectID, req.FilePath, req.Branch, userID)
	if err != nil {
		if errors.Is(err, service.ErrFileLocked) {
			writeJSON(w, http.StatusConflict, errorBody("FILE_LOCKED", err.Error()))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to acquire lock"))
		return
	}

	writeJSON(w, http.StatusCreated, lock)
}

// Release unlocks a file path.
func (h *LockHandler) Release(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := middleware.UserIDFromContext(r.Context())

	var req struct {
		FilePath string `json:"file_path"`
		Force    bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	if req.FilePath == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "file_path is required"))
		return
	}

	if err := h.locks.Release(r.Context(), projectID, req.FilePath, userID, req.Force); err != nil {
		if errors.Is(err, service.ErrNotLockOwner) {
			writeJSON(w, http.StatusForbidden, errorBody("NOT_LOCK_OWNER", "You do not own this lock (use force to override)"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to release lock"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "unlocked"})
}

// List returns all active locks for a project.
func (h *LockHandler) List(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	locks, err := h.locks.List(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to list locks"))
		return
	}

	writeJSON(w, http.StatusOK, locks)
}
