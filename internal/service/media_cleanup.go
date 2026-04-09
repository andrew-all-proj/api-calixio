package service

import (
	"context"
	"strings"
	"time"

	"calixio/internal/repository"

	"go.uber.org/zap"
)

type MediaCleanupService struct {
	mediaRepo  repository.MediaRepository
	storage    *StorageService
	logger     *zap.Logger
	batchSize  int
	runTimeout time.Duration
}

type NewMediaCleanupServiceInput struct {
	MediaRepo  repository.MediaRepository
	Storage    *StorageService
	Logger     *zap.Logger
	BatchSize  int
	RunTimeout time.Duration
}

func NewMediaCleanupService(in NewMediaCleanupServiceInput) *MediaCleanupService {
	logger := in.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	batchSize := in.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	runTimeout := in.RunTimeout
	if runTimeout <= 0 {
		runTimeout = 30 * time.Minute
	}

	return &MediaCleanupService{
		mediaRepo:  in.MediaRepo,
		storage:    in.Storage,
		logger:     logger,
		batchSize:  batchSize,
		runTimeout: runTimeout,
	}
}

func (s *MediaCleanupService) RunOnce(ctx context.Context) (scannedCount, deletedCount, failedCount int, err error) {
	prefixByID, err := s.storage.ListMediaPrefixes(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	if len(prefixByID) == 0 {
		return 0, 0, 0, nil
	}

	ids := make([]string, 0, len(prefixByID))
	for id := range prefixByID {
		if strings.TrimSpace(id) == "" {
			continue
		}
		ids = append(ids, id)
	}
	scannedCount = len(ids)

	existing := make(map[string]struct{}, len(ids))
	for start := 0; start < len(ids); start += s.batchSize {
		end := start + s.batchSize
		if end > len(ids) {
			end = len(ids)
		}

		chunk, listErr := s.mediaRepo.ListExistingIDs(ctx, ids[start:end])
		if listErr != nil {
			return scannedCount, deletedCount, failedCount, listErr
		}
		for _, id := range chunk {
			existing[id] = struct{}{}
		}
	}

	for _, id := range ids {
		if _, ok := existing[id]; ok {
			continue
		}

		prefix := prefixByID[id]
		if err := s.storage.DeleteObjectsByPrefix(ctx, prefix); err != nil {
			failedCount++
			s.logger.Error("orphaned media cleanup delete failed",
				zap.String("media_id", id),
				zap.String("prefix", prefix),
				zap.Error(err),
			)
			continue
		}

		deletedCount++
		s.logger.Info("orphaned media cleanup deleted",
			zap.String("media_id", id),
			zap.String("prefix", prefix),
		)
	}

	return scannedCount, deletedCount, failedCount, nil
}

func (s *MediaCleanupService) RunDaily() {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), s.runTimeout)
			scannedCount, deletedCount, failedCount, err := s.RunOnce(ctx)
			cancel()

			if err != nil {
				s.logger.Error("orphaned media cleanup failed", zap.Error(err))
				continue
			}

			s.logger.Info(
				"orphaned media cleanup finished",
				zap.Int("scanned_media", scannedCount),
				zap.Int("deleted_media", deletedCount),
				zap.Int("failed_media", failedCount),
			)
		}
	}()
}
