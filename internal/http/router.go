package http

import (
	"net/http"
	"time"

	"calixio/internal/http/authn"
	authhandlers "calixio/internal/http/handlers/auth"
	roomhandlers "calixio/internal/http/handlers/rooms"
	webhookhandlers "calixio/internal/http/handlers/webhook"
	"calixio/internal/repository"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"
)

func NewRouter(
	authHandler *authhandlers.Handler,
	roomHandler *roomhandlers.Handler,
	webhookHandler *webhookhandlers.Handler,
	jwt *authn.JWTService,
	tokens repository.SessionRepository,
	logger *zap.Logger,
	corsOrigins []string,
) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		ExposedHeaders:   []string{"Link", "Set-Cookie"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(LoggingMiddleware(logger))

	r.Post("/auth/login", authHandler.Login)
	r.Post("/auth/register", authHandler.Register)
	r.Post("/auth/refresh", authHandler.Refresh)

	r.Route("/rooms", func(r chi.Router) {
		r.Post("/{id}/join", roomHandler.JoinRoom)
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(jwt, tokens))
			r.Post("/", roomHandler.CreateRoom)
			r.Post("/{id}/end", roomHandler.EndRoom)
		})
	})

	r.Post("/livekit/webhook", webhookHandler.LiveKitWebhook)

	return r
}
