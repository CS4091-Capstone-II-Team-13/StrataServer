package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"strata.themrdt.org/dev/server/internal/service"
)

type AuthHandler struct {
	auth *service.AuthService
}

func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	if req.Username == "" || req.Email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("MISSING_FIELDS", "username, email, and password are required"))
		return
	}
	if len(req.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, errorBody("WEAK_PASSWORD", "Password must be at least 8 characters"))
		return
	}

	user, err := h.auth.Register(r.Context(), req.Username, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrUserExists) {
			writeJSON(w, http.StatusConflict, errorBody("USER_EXISTS", "Username or email already exists"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Failed to create user"))
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	tokens, err := h.auth.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			writeJSON(w, http.StatusUnauthorized, errorBody("AUTH_INVALID_CREDENTIALS", "Invalid username or password"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("INTERNAL_ERROR", "Login failed"))
		return
	}

	writeJSON(w, http.StatusOK, tokens)
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("INVALID_REQUEST", "Invalid JSON body"))
		return
	}

	tokens, err := h.auth.RefreshAccessToken(r.Context(), req.RefreshToken)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, errorBody("AUTH_REFRESH_EXPIRED", "Invalid or expired refresh token"))
		return
	}

	writeJSON(w, http.StatusOK, tokens)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// TODO: Invalidate refresh token in database.
	// For v1, the client just discards its tokens.
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}
