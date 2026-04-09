package main

import (
	"calixio/internal/config"
	"calixio/internal/http/authn"
	authhandlers "calixio/internal/http/handlers/auth"
	filehandlers "calixio/internal/http/handlers/files"
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

	logger.Info("postgres ping check started")
	pingStartedAt := time.Now()
	if err := pool.Ping(ctx); err != nil {
		logger.Fatal("postgres ping", zap.Error(err))
	}
	logger.Info("postgres ping check passed", zap.Duration("latency", time.Since(pingStartedAt)))

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
	mediaRepo := repository.NewPostgresMediaRepository(pool)
	userRepo := repository.NewPostgresUserRepository(pool)
	sessionRepo := repository.NewPostgresSessionRepository(pool)
	roomSvc := service.NewRoomService(roomRepo, mediaRepo, lkClient)
	playbackSvc := service.NewRoomPlaybackService(roomRepo, redisClient)
	webhookSvc := service.NewWebhookService(redisClient)

	storageSvc, err := service.NewStorageService(ctx, cfg)
	if err != nil {
		logger.Fatal("storage init", zap.Error(err))
	}

	var transcoderSvc *service.MediaTranscoderService
	if cfg.Transcoding.Enabled {
		transcoderSvc, err = service.NewMediaTranscoderService(service.NewMediaTranscoderServiceInput{
			MediaRepo:       mediaRepo,
			Storage:         storageSvc,
			FFmpegPath:      cfg.Transcoding.FFmpegPath,
			FFprobePath:     cfg.Transcoding.FFprobePath,
			WorkDir:         cfg.Transcoding.WorkDir,
			SegmentDuration: cfg.Transcoding.HLSSegmentSec,
			QueueSize:       cfg.Transcoding.QueueSize,
			JobTimeout:      cfg.Transcoding.JobTimeout,
			Logger:          logger,
		})
		if err != nil {
			logger.Fatal("transcoder init", zap.Error(err))
		}
	}

	mediaUploadSvc := service.NewMediaUploadService(service.NewMediaUploadServiceInput{
		MediaRepo:         mediaRepo,
		Storage:           storageSvc,
		Transcoder:        transcoderSvc,
		Cache:             redisClient,
		MaxSizeBytes:      cfg.AWS.MaxUploadBytes,
		AllowedMimeTypes:  cfg.AWS.AllowedMIMEs,
		PresignURLTTL:     cfg.AWS.PresignTTL,
		PlaybackSignedTTL: cfg.MediaPlayback.SignedTTL,
	})
	mediaCleanupSvc := service.NewMediaCleanupService(service.NewMediaCleanupServiceInput{
		MediaRepo: mediaRepo,
		Storage:   storageSvc,
		Logger:    logger,
	})

	jwtSvc := authn.NewJWTService(cfg.JWTSecret, cfg.AccessTTL)
	authSvc := service.NewAuthService(userRepo, sessionRepo, jwtSvc, cfg.AccessTTL, cfg.RefreshTTL)
	authHandler := authhandlers.NewHandler(authSvc, jwtSvc, logger)
	fileHandler := filehandlers.NewHandler(mediaUploadSvc, logger)
	roomHandler := roomhandlers.NewHandler(roomSvc, mediaUploadSvc, playbackSvc, jwtSvc, logger)
	webhookHandler := webhookhandlers.NewHandler(webhookSvc, lkClient, logger)
	router := httpserver.NewRouter(authHandler, roomHandler, fileHandler, webhookHandler, jwtSvc, sessionRepo, logger, cfg.CORSOrigins)

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

	startStaleActiveRoomsCloser(roomSvc, logger, 24*time.Hour, time.Hour)
	mediaCleanupSvc.RunDaily()

	waitForShutdown(logger, srv)
}

func startStaleActiveRoomsCloser(roomSvc *service.RoomService, logger *zap.Logger, minActiveAge, runEvery time.Duration) {
	if minActiveAge <= 0 {
		minActiveAge = 24 * time.Hour
	}
	if runEvery <= 0 {
		runEvery = time.Hour
	}

	go func() {
		ticker := time.NewTicker(runEvery)
		defer ticker.Stop()

		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			closedCount, failedCount, err := roomSvc.EndStaleActiveRooms(ctx, minActiveAge)
			cancel()
			if err != nil {
				logger.Error("stale room closer failed", zap.Error(err))
			} else {
				logger.Info(
					"stale room closer finished",
					zap.Int("closed_rooms", closedCount),
					zap.Int("failed_rooms", failedCount),
					zap.Duration("min_active_age", minActiveAge),
				)
			}
		}
	}()
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
