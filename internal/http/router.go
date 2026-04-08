package http

import (
	"net/http"
	"time"

	"calixio/internal/http/authn"
	authhandlers "calixio/internal/http/handlers/auth"
	filehandlers "calixio/internal/http/handlers/files"
	roomhandlers "calixio/internal/http/handlers/rooms"
	webhookhandlers "calixio/internal/http/handlers/webhook"
	httpmiddleware "calixio/internal/http/middleware"
	"calixio/internal/repository"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"
)

func NewRouter(
	authHandler *authhandlers.Handler,
	roomHandler *roomhandlers.Handler,
	fileHandler *filehandlers.Handler,
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
	allowedOrigins := append([]string{}, corsOrigins...)
	allowedOrigins = append(allowedOrigins, "https://calixio.managetlg.com")
	hasWildcardOrigin := false
	filtered := make([]string, 0, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin == "*" {
			hasWildcardOrigin = true
			continue
		}
		filtered = append(filtered, origin)
	}

	corsOptions := cors.Options{
		AllowedOrigins:   filtered,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		ExposedHeaders:   []string{"Link", "Set-Cookie"},
		AllowCredentials: true,
		MaxAge:           300,
	}
	if hasWildcardOrigin {
		corsOptions.AllowOriginFunc = func(r *http.Request, origin string) bool {
			return true
		}
	}
	r.Use(cors.Handler(corsOptions))
	r.Use(httpmiddleware.LoggingMiddleware(logger))

	r.Post("/auth/login", authHandler.Login)
	r.Post("/auth/register", authHandler.Register)
	r.Post("/auth/refresh", authHandler.Refresh)
	r.Get("/media/playback/{token}/index.m3u8", fileHandler.GetPlaybackManifest)

	r.Route("/rooms", func(r chi.Router) {
		r.Post("/{id}/join", roomHandler.JoinRoom)
		r.Get("/{id}/playback", roomHandler.GetRoomPlaybackState)
		r.Group(func(r chi.Router) {
			r.Use(httpmiddleware.AuthMiddleware(jwt, tokens))
			r.Get("/", roomHandler.ListRooms)
			r.Post("/", roomHandler.CreateRoom)
			r.Post("/{id}/state", roomHandler.UpdateRoomState)
			r.Post("/{id}/playback", roomHandler.UpdateRoomPlaybackState)
			r.Post("/{id}/end", roomHandler.EndRoom)
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(httpmiddleware.AuthMiddleware(jwt, tokens))
		r.Get("/media", fileHandler.ListMedia)
		r.Get("/media/{id}/playback", fileHandler.GetPlayback)
		r.Delete("/media/{id}", fileHandler.DeleteMedia)
		r.Post("/media/upload/init", fileHandler.InitMediaUpload)
		r.Post("/media/upload/complete", fileHandler.CompleteMediaUpload)
	})

	r.Post("/livekit/webhook", webhookHandler.LiveKitWebhook)

	return r
}
