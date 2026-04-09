package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"calixio/internal/repository"

	"go.uber.org/zap"
)

type MediaTranscoderService struct {
	mediaRepo       repository.MediaRepository
	storage         *StorageService
	ffmpegPath      string
	ffprobePath     string
	workDir         string
	segmentDuration int
	jobTimeout      time.Duration
	jobQueue        chan string
	logger          *zap.Logger
}

type hlsEncodingProfile struct {
	name      string
	preset    string
	crf       int
	maxrate   string
	bufsize   string
	audioBitr string
	threads   int
}

type NewMediaTranscoderServiceInput struct {
	MediaRepo       repository.MediaRepository
	Storage         *StorageService
	FFmpegPath      string
	FFprobePath     string
	WorkDir         string
	SegmentDuration int
	QueueSize       int
	JobTimeout      time.Duration
	Logger          *zap.Logger
}

func NewMediaTranscoderService(in NewMediaTranscoderServiceInput) (*MediaTranscoderService, error) {
	ffmpegPath := strings.TrimSpace(in.FFmpegPath)
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	ffprobePath := strings.TrimSpace(in.FFprobePath)
	if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}
	if _, err := exec.LookPath(ffmpegPath); err != nil {
		return nil, fmt.Errorf("ffmpeg binary is not available: %w", err)
	}
	if _, err := exec.LookPath(ffprobePath); err != nil {
		return nil, fmt.Errorf("ffprobe binary is not available: %w", err)
	}

	segmentDuration := in.SegmentDuration
	if segmentDuration <= 0 {
		segmentDuration = 6
	}
	queueSize := in.QueueSize
	if queueSize <= 0 {
		queueSize = 32
	}
	jobTimeout := in.JobTimeout
	if jobTimeout <= 0 {
		jobTimeout = 2 * time.Hour
	}
	logger := in.Logger
	if logger == nil {
		logger = zap.NewNop()
	}

	svc := &MediaTranscoderService{
		mediaRepo:       in.MediaRepo,
		storage:         in.Storage,
		ffmpegPath:      ffmpegPath,
		ffprobePath:     ffprobePath,
		workDir:         strings.TrimSpace(in.WorkDir),
		segmentDuration: segmentDuration,
		jobTimeout:      jobTimeout,
		jobQueue:        make(chan string, queueSize),
		logger:          logger,
	}

	go svc.worker()
	return svc, nil
}

func (s *MediaTranscoderService) Enqueue(mediaID string) error {
	trimmed := strings.TrimSpace(mediaID)
	if trimmed == "" {
		return errors.New("media id is required")
	}

	select {
	case s.jobQueue <- trimmed:
		return nil
	default:
		return errors.New("transcoding queue is full")
	}
}

func (s *MediaTranscoderService) worker() {
	for mediaID := range s.jobQueue {
		ctx, cancel := context.WithTimeout(context.Background(), s.jobTimeout)
		err := s.processMedia(ctx, mediaID)
		cancel()

		if err != nil {
			s.logger.Error("transcoding failed", zap.String("media_id", mediaID), zap.Error(err))
			if markErr := s.mediaRepo.UpdateStatus(context.Background(), mediaID, repository.MediaFailed); markErr != nil {
				s.logger.Error("mark media failed", zap.String("media_id", mediaID), zap.Error(markErr))
			}
		}
	}
}

func (s *MediaTranscoderService) processMedia(ctx context.Context, mediaID string) error {
	media, err := s.mediaRepo.GetByID(ctx, mediaID)
	if err != nil {
		return err
	}

	baseDirs := s.tempBaseDirs()
	var lastErr error
	for idx, baseDir := range baseDirs {
		tmpDir, mkErr := s.makeTempWorkspace(baseDir)
		if mkErr != nil {
			lastErr = mkErr
			if isNoSpaceErr(mkErr) && idx < len(baseDirs)-1 {
				s.logger.Warn("transcoding workspace base is full; trying fallback",
					zap.String("media_id", media.ID),
					zap.String("base_dir", baseDir),
					zap.Error(mkErr),
				)
				continue
			}
			return mkErr
		}

		processErr := s.processMediaInWorkspace(ctx, media, tmpDir)
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			s.logger.Warn("failed to cleanup transcoding workspace", zap.String("path", tmpDir), zap.Error(rmErr))
		}
		if processErr == nil {
			return nil
		}
		lastErr = processErr
		if isNoSpaceErr(processErr) && idx < len(baseDirs)-1 {
			s.logger.Warn("transcoding failed due to full workspace; trying fallback",
				zap.String("media_id", media.ID),
				zap.String("base_dir", baseDir),
				zap.Error(processErr),
			)
			continue
		}
		return processErr
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("transcoding failed with no available workspace")
}

func (s *MediaTranscoderService) processMediaInWorkspace(ctx context.Context, media repository.Media, tmpDir string) error {
	srcPath := filepath.Join(tmpDir, "input"+filepath.Ext(media.OriginalName))
	if err := s.withRetry(ctx, "download source", media.ID, func() error {
		return s.storage.DownloadObjectToFile(ctx, media.StorageKey, srcPath)
	}); err != nil {
		return fmt.Errorf("download source: %w", err)
	}

	durationSec, err := s.probeDurationSec(ctx, srcPath)
	if err != nil {
		return err
	}

	hlsDir := filepath.Join(tmpDir, "hls")
	if err := os.MkdirAll(hlsDir, 0o755); err != nil {
		return err
	}

	manifestPath := filepath.Join(hlsDir, "index.m3u8")
	segmentPattern := filepath.Join(hlsDir, "segment_%05d.ts")

	s.logger.Info("media conversion started",
		zap.String("media_id", media.ID),
		zap.String("source_key", media.StorageKey),
		zap.Int("duration_sec", durationSec),
	)
	if err := s.runFFmpegHLS(ctx, srcPath, manifestPath, segmentPattern, durationSec); err != nil {
		return err
	}

	previewPath := filepath.Join(tmpDir, "preview.jpg")

	s.logger.Info("media upload started",
		zap.String("media_id", media.ID),
		zap.String("target_prefix", path.Join("users", media.OwnerUserID, "media", media.ID)),
	)
	previewURL, err := s.createAndUploadPreview(ctx, srcPath, previewPath, media)
	if err != nil {
		return err
	}

	prefix := path.Join("users", media.OwnerUserID, "media", media.ID, "hls")
	if err := s.uploadHLSOutput(ctx, hlsDir, prefix, media.ID); err != nil {
		return err
	}

	playbackKey := path.Join(prefix, "index.m3u8")
	playbackURL := s.storage.generateObjectURL(playbackKey)

	if err := s.mediaRepo.UpdateTranscodeResult(
		ctx,
		media.ID,
		playbackURL,
		previewURL,
		&durationSec,
		repository.MediaReady,
	); err != nil {
		return err
	}

	s.logger.Info("media upload completed",
		zap.String("media_id", media.ID),
		zap.String("playback_url", playbackURL),
	)

	return nil
}

func (s *MediaTranscoderService) tempBaseDirs() []string {
	out := make([]string, 0, 4)
	seen := map[string]struct{}{}
	add := func(dir string) {
		trimmed := strings.TrimSpace(dir)
		if trimmed == "" {
			return
		}
		cleaned := filepath.Clean(trimmed)
		if _, exists := seen[cleaned]; exists {
			return
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}

	add(s.workDir)
	add(os.TempDir())
	add("/var/tmp")
	add(".")

	return out
}

func (s *MediaTranscoderService) makeTempWorkspace(baseDir string) (string, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", fmt.Errorf("prepare transcoding temp base %q: %w", baseDir, err)
	}
	tmpDir, err := os.MkdirTemp(baseDir, "calixio-transcode-*")
	if err != nil {
		return "", fmt.Errorf("create transcoding workspace in %q: %w", baseDir, err)
	}
	return tmpDir, nil
}

func isNoSpaceErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "no space left on device")
}

func (s *MediaTranscoderService) runFFmpegHLS(ctx context.Context, srcPath, manifestPath, segmentPattern string, durationSec int) error {
	profile := selectHLSEncodingProfile(durationSec)
	args := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-loglevel", "warning",
		"-i", srcPath,
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c:v", "libx264",
		"-preset", profile.preset,
		"-profile:v", "main",
		"-level", "4.0",
		"-pix_fmt", "yuv420p",
		"-crf", strconv.Itoa(profile.crf),
		"-maxrate", profile.maxrate,
		"-bufsize", profile.bufsize,
		"-threads", strconv.Itoa(profile.threads),
		"-c:a", "aac",
		"-b:a", profile.audioBitr,
		"-ac", "2",
		"-f", "hls",
		"-hls_time", strconv.Itoa(s.segmentDuration),
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", segmentPattern,
		manifestPath,
	}

	cmd := exec.CommandContext(ctx, s.ffmpegPath, args...)
	stderr := &tailBuffer{maxBytes: 64 << 10}
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		return formatFFmpegError("ffmpeg hls", err, stderr.String())
	}
	return nil
}

func selectHLSEncodingProfile(durationSec int) hlsEncodingProfile {
	const longMediaThresholdSec = 90 * 60
	if durationSec >= longMediaThresholdSec {
		return hlsEncodingProfile{
			name:      "long-form-safe",
			preset:    "superfast",
			crf:       23,
			maxrate:   "3500k",
			bufsize:   "7000k",
			audioBitr: "96k",
			threads:   2,
		}
	}
	return hlsEncodingProfile{
		name:      "default",
		preset:    "veryfast",
		crf:       21,
		maxrate:   "5000k",
		bufsize:   "10000k",
		audioBitr: "128k",
		threads:   0,
	}
}

func (s *MediaTranscoderService) createAndUploadPreview(ctx context.Context, srcPath, previewPath string, media repository.Media) (*string, error) {
	args := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-loglevel", "warning",
		"-ss", "00:00:01",
		"-i", srcPath,
		"-frames:v", "1",
		"-vf", "scale=640:-2",
		previewPath,
	}
	cmd := exec.CommandContext(ctx, s.ffmpegPath, args...)
	stderr := &tailBuffer{maxBytes: 32 << 10}
	cmd.Stdout = io.Discard
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		return nil, formatFFmpegError("ffmpeg preview", err, stderr.String())
	}

	previewKey := path.Join("users", media.OwnerUserID, "media", media.ID, "preview", "preview.jpg")
	if err := s.withRetry(ctx, "upload preview", media.ID, func() error {
		return s.storage.UploadFilePublic(ctx, previewKey, "image/jpeg", previewPath)
	}); err != nil {
		return nil, err
	}
	previewURL := s.storage.generateObjectURL(previewKey)
	return &previewURL, nil
}

func (s *MediaTranscoderService) uploadHLSOutput(ctx context.Context, hlsDir, prefix, mediaID string) error {
	entries, err := os.ReadDir(hlsDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()
		localPath := filepath.Join(hlsDir, fileName)
		targetKey := path.Join(prefix, fileName)

		contentType := "application/octet-stream"
		switch {
		case strings.HasSuffix(fileName, ".m3u8"):
			contentType = "application/vnd.apple.mpegurl"
		case strings.HasSuffix(fileName, ".ts"):
			contentType = "video/mp2t"
		}

		if err := s.withRetry(ctx, "upload hls segment", mediaID, func() error {
			return s.storage.UploadFilePublic(ctx, targetKey, contentType, localPath)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *MediaTranscoderService) withRetry(ctx context.Context, opName, mediaID string, fn func() error) error {
	const maxAttempts = 4
	backoff := time.Second
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
		}

		if attempt == maxAttempts {
			break
		}

		s.logger.Warn("transcoder operation retry",
			zap.String("operation", opName),
			zap.String("media_id", mediaID),
			zap.Int("attempt", attempt),
			zap.Error(lastErr),
		)

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
	}

	return lastErr
}

type tailBuffer struct {
	maxBytes int
	buf      []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	if b.maxBytes <= 0 {
		return len(p), nil
	}
	if len(p) >= b.maxBytes {
		b.buf = append(b.buf[:0], p[len(p)-b.maxBytes:]...)
		return len(p), nil
	}
	needed := len(b.buf) + len(p) - b.maxBytes
	if needed > 0 {
		b.buf = append(b.buf[:0], b.buf[needed:]...)
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *tailBuffer) String() string {
	return strings.TrimSpace(string(b.buf))
}

func formatFFmpegError(prefix string, err error, stderr string) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() && status.Signal() == syscall.SIGKILL {
			if stderr == "" {
				return fmt.Errorf("%s: process was killed with SIGKILL; likely OOM kill or container/node resource limit", prefix)
			}
			return fmt.Errorf("%s: process was killed with SIGKILL; likely OOM kill or container/node resource limit: %s", prefix, stderr)
		}
	}
	if stderr == "" {
		return fmt.Errorf("%s: %w", prefix, err)
	}
	return fmt.Errorf("%s: %w: %s", prefix, err, stderr)
}

func (s *MediaTranscoderService) probeDurationSec(ctx context.Context, srcPath string) (int, error) {
	cmd := exec.CommandContext(
		ctx,
		s.ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		srcPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ffprobe duration: %w: %s", err, strings.TrimSpace(string(out)))
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0, nil
	}
	durationFloat, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, err
	}
	if durationFloat <= 0 {
		return 0, nil
	}
	return int(math.Round(durationFloat)), nil
}
