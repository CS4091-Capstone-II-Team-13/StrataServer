package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"strata.themrdt.org/dev/server/internal/service"
)

type PullHandler struct {
	chunks   *service.ChunkService
	versions *service.VersionService
}

func NewPullHandler(chunks *service.ChunkService, versions *service.VersionService) *PullHandler {
	return &PullHandler{chunks: chunks, versions: versions}
}

// Diff computes the difference between two commits.
func (h *PullHandler) Diff(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	var req struct {
		FromCommitID *string `json:"from_commit_id"`
		ToCommitID   string  `json:"to_commit_id"`
		Ref          string  `json:"ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	// If ref is provided instead of to_commit_id, resolve it.
	toCommitID := req.ToCommitID
	if toCommitID == "" && req.Ref != "" {
		ref, err := h.versions.ResolveRef(r.Context(), projectID, req.Ref)
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody("REF_NOT_FOUND", fmt.Sprintf("Ref %q not found", req.Ref)))
			return
		}
		toCommitID = ref.CommitID
	}

	if toCommitID == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "to_commit_id or ref is required"))
		return
	}

	result, err := h.versions.ComputeDiff(r.Context(), &service.DiffRequest{
		ProjectID:    projectID,
		FromCommitID: req.FromCommitID,
		ToCommitID:   toCommitID,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to compute diff"))
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// DownloadChunk streams a single chunk by hash.
func (h *PullHandler) DownloadChunk(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if len(hash) != 64 {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_HASH", "Hash must be 64 hex characters"))
		return
	}

	reader, size, err := h.chunks.Download(r.Context(), hash)
	if err != nil {
		if errors.Is(err, service.ErrChunkNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("CHUNK_NOT_FOUND", "Chunk not found"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to retrieve chunk"))
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Chunk-Hash", hash)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))

	io.Copy(w, reader)
}
