package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"strata.themrdt.org/dev/server/internal/middleware"
	"strata.themrdt.org/dev/server/internal/service"
)

type ProjectHandler struct {
	projects *service.ProjectService
}

func NewProjectHandler(projects *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{projects: projects}
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "name is required"))
		return
	}

	userID := middleware.UserIDFromContext(r.Context())
	project, err := h.projects.Create(r.Context(), req.Name, req.Description, userID)
	if err != nil {
		if errors.Is(err, service.ErrProjectExists) {
			writeJSON(w, http.StatusConflict, errorBody("PROJECT_EXISTS", "Project name already exists"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to create project"))
		return
	}

	writeJSON(w, http.StatusCreated, project)
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFromContext(r.Context())
	projects, err := h.projects.List(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to list projects"))
		return
	}

	writeJSON(w, http.StatusOK, projects)
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")
	project, err := h.projects.Get(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("PROJECT_NOT_FOUND", "Project not found"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to get project"))
		return
	}

	writeJSON(w, http.StatusOK, project)
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectID")

	if err := h.projects.Delete(r.Context(), projectID); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to delete project"))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
