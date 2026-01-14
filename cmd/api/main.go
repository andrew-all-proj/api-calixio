package main

import (
	"calixio/internal/config"
	"calixio/internal/http/authn"
	authhandlers "calixio/internal/http/handlers/auth"
	roomhandlers "calixio/internal/http/handlers/rooms"
	webhookhandlers "calixio/internal/http/handlers/webhook"
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpserver "calixio/internal/http"
	"calixio/internal/livekit"
	"calixio/internal/repository"
	"calixio/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	var logger *zap.Logger
	if cfg.Env == "prod" {
		logger, err = zap.NewProduction()
	} else {
		logger, err = zap.NewDevelopment()
	}
	if err != nil {
		panic(err)
	}

	defer func() { _ = logger.Sync() }()

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("postgres connect", zap.Error(err))
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Fatal("postgres ping", zap.Error(err))
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
		DB:   cfg.RedisDB,
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Fatal("redis ping", zap.Error(err))
	}

	lkClient := livekit.NewClient(
		cfg.LiveKit.Host,
		cfg.LiveKit.APIKey,
		cfg.LiveKit.APISecret,
		cfg.LiveKit.WebhookSecret,
		cfg.LiveKit.RoomAutoTimeout,
		cfg.LiveKit.TokenTTL,
	)

	roomRepo := repository.NewPostgresRoomRepository(pool)
	userRepo := repository.NewPostgresUserRepository(pool)
	sessionRepo := repository.NewPostgresSessionRepository(pool)
	roomSvc := service.NewRoomService(roomRepo, lkClient)
	webhookSvc := service.NewWebhookService(redisClient)

	jwtSvc := authn.NewJWTService(cfg.JWTSecret, cfg.AccessTTL)
	authSvc := service.NewAuthService(userRepo, sessionRepo, jwtSvc, cfg.AccessTTL, cfg.RefreshTTL)
	authHandler := authhandlers.NewHandler(authSvc, jwtSvc, logger)
	roomHandler := roomhandlers.NewHandler(roomSvc, jwtSvc, logger)
	webhookHandler := webhookhandlers.NewHandler(webhookSvc, lkClient, logger)
	router := httpserver.NewRouter(authHandler, roomHandler, webhookHandler, jwtSvc, sessionRepo, logger, cfg.CORSOrigins)

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("http server listening", zap.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("http server error", zap.Error(err))
		}
	}()

	waitForShutdown(logger, srv)
}

func waitForShutdown(logger *zap.Logger, srv *http.Server) {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("shutting down")
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", zap.Error(err))
	}
}
