package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	ContextKeyUserID   contextKey = "user_id"
	ContextKeyUsername contextKey = "username"
	ContextKeyRole     contextKey = "role"
)

// Auth returns middleware that validates JWT access tokens from the
// Authorization header and injects claims into the request context.
func Auth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "AUTH_MISSING_TOKEN", "Authorization header is required")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				writeError(w, http.StatusUnauthorized, "AUTH_INVALID_FORMAT", "Authorization header must be: Bearer <token>")
				return
			}

			tokenStr := parts[1]
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(jwtSecret), nil
			})

			if err != nil || !token.Valid {
				writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Invalid or expired access token")
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				writeError(w, http.StatusUnauthorized, "AUTH_TOKEN_INVALID", "Could not parse token claims")
				return
			}

			// Extract claims into context.
			ctx := r.Context()
			if userID, ok := claims["sub"].(string); ok {
				ctx = context.WithValue(ctx, ContextKeyUserID, userID)
			}
			if username, ok := claims["username"].(string); ok {
				ctx = context.WithValue(ctx, ContextKeyUsername, username)
			}
			if role, ok := claims["role"].(string); ok {
				ctx = context.WithValue(ctx, ContextKeyRole, role)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext extracts the user ID from the request context.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyUserID).(string); ok {
		return v
	}
	return ""
}

// UsernameFromContext extracts the username from the request context.
func UsernameFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyUsername).(string); ok {
		return v
	}
	return ""
}

// RoleFromContext extracts the role from the request context.
func RoleFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ContextKeyRole).(string); ok {
		return v
	}
	return ""
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}`))
}
