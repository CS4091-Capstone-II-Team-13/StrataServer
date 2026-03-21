package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"strata.themrdt.org/dev/server/internal/middleware"
	"strata.themrdt.org/dev/server/internal/service"
)

type PushHandler struct {
	chunks   *service.ChunkService
	versions *service.VersionService
	locks    *service.LockService
}

func NewPushHandler(chunks *service.ChunkService, versions *service.VersionService, locks *service.LockService) *PushHandler {
	return &PushHandler{chunks: chunks, versions: versions, locks: locks}
}

// CheckChunks accepts a list of hashes and returns which ones already exist.
func (h *PushHandler) CheckChunks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Hashes []string `json:"hashes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	if len(req.Hashes) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "hashes array is required"))
		return
	}

	existing, needed, err := h.chunks.CheckExisting(r.Context(), req.Hashes)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to check chunks"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"existing": existing,
		"needed":   needed,
	})
}

// UploadChunk accepts raw bytes with the expected hash in a header.
func (h *PushHandler) UploadChunk(w http.ResponseWriter, r *http.Request) {
	expectedHash := r.Header.Get("X-Chunk-Hash")
	if expectedHash == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_HEADER", "X-Chunk-Hash header is required"))
		return
	}

	// Read body with a 5MB limit (max chunk + overhead).
	body := http.MaxBytesReader(w, r.Body, 5*1024*1024)
	data, err := io.ReadAll(body)
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody("CHUNK_TOO_LARGE", "Chunk exceeds maximum size"))
		return
	}

	if err := h.chunks.Upload(r.Context(), expectedHash, data); err != nil {
		if errors.Is(err, service.ErrChunkHashMismatch) {
			writeJSON(w, http.StatusBadRequest, errorBody("CHUNK_HASH_MISMATCH", "Uploaded chunk hash does not match expected hash"))
			return
		}
		if errors.Is(err, service.ErrChunkTooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, errorBody("CHUNK_TOO_LARGE", "Chunk exceeds maximum size"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to store chunk"))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// Commit finalizes a push with file manifests.
func (h *PushHandler) Commit(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	userID := middleware.UserIDFromContext(r.Context())

	var req struct {
		ParentCommitID *string                   `json:"parent_commit_id"`
		Message        string                    `json:"message"`
		Ref            string                    `json:"ref"`
		Files          []service.CommitFileEntry `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "message is required"))
		return
	}
	if len(req.Files) == 0 {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "at least one file is required"))
		return
	}

	// Check file locks — warn if any files are locked by another user.
	if h.locks != nil {
		var paths []string
		for _, f := range req.Files {
			paths = append(paths, f.Path)
		}
		conflicting, err := h.locks.CheckPushPaths(r.Context(), projectID, paths, userID)
		if err == nil && len(conflicting) > 0 {
			lockInfo := make([]map[string]string, len(conflicting))
			for i, l := range conflicting {
				lockInfo[i] = map[string]string{
					"file_path": l.FilePath,
					"locked_by": l.LockedByUsername,
					"branch":    l.Branch,
				}
			}
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error": map[string]interface{}{
					"code":    "FILES_LOCKED",
					"message": "One or more files are locked by another user",
					"locks":   lockInfo,
				},
			})
			return
		}
	}

	result, err := h.versions.CreateCommit(r.Context(), &service.CommitRequest{
		ProjectID:      projectID,
		ParentCommitID: req.ParentCommitID,
		AuthorID:       userID,
		Message:        req.Message,
		Ref:            req.Ref,
		Files:          req.Files,
	})
	if err != nil {
		if errors.Is(err, service.ErrCommitConflict) {
			writeJSON(w, http.StatusConflict, errorBody("COMMIT_CONFLICT", "Parent commit does not match current ref head — pull and retry"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to create commit"))
		return
	}

	writeJSON(w, http.StatusCreated, result)
}
