package authn

import (
	"context"
	"net/http"
	"strings"

	"calixio/internal/http/httputil"
)

type ctxKey string

const userIDKey ctxKey = "user_id"

func AuthMiddleware(jwt *JWTService) func(http.Handler) http.Handler {
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
			ctx := context.WithValue(r.Context(), userIDKey, claims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserIDFromContext(ctx context.Context) string {
	val := ctx.Value(userIDKey)
	if val == nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}
