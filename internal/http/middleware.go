package http

import (
	"net/http"
	"strings"
	"time"

	"calixio/internal/http/authn"
	httputil "calixio/internal/http/httputil"
	"calixio/internal/repository"

	"go.uber.org/zap"
)

func LoggingMiddleware(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(ww, r)

			logger.Info("http request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", ww.status),
				zap.Duration("duration", time.Since(start)),
			)
		})
	}
}

func AuthMiddleware(jwt *authn.JWTService, sessions repository.SessionRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				httputil.RespondError(w, http.StatusUnauthorized, "missing_auth")
				return
			}
			parts := strings.SplitN(auth, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				httputil.RespondError(w, http.StatusUnauthorized, "invalid_auth")
				return
			}

			claims, err := jwt.ParseToken(parts[1])
			if err != nil {
				httputil.RespondError(w, http.StatusUnauthorized, "invalid_token")
				return
			}

			if claims.ID != "" && sessions != nil {
				revoked, err := sessions.IsAccessRevoked(r.Context(), claims.ID)
				if err != nil {
					httputil.RespondError(w, http.StatusInternalServerError, "token_check_failed")
					return
				}
				if revoked {
					httputil.RespondError(w, http.StatusUnauthorized, "token_revoked")
					return
				}
			}

			ctx := authn.ContextWithUserID(r.Context(), claims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
