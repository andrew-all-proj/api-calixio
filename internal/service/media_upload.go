package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"calixio/internal/repository"
	"github.com/redis/go-redis/v9"
)

var (
	ErrInvalidUploadInput = errors.New("invalid upload input")
	ErrForbiddenMedia     = errors.New("forbidden media")
	ErrMediaNotReady      = errors.New("media is not ready")
	ErrUploadedObjectNotFound = errors.New("uploaded object not found")
	ErrStorageDelete      = errors.New("media storage delete failed")
	ErrInvalidManifestKey = errors.New("invalid playback manifest token")
)

type MediaUploadService struct {
	mediaRepo        repository.MediaRepository
	storage          *StorageService
	transcoder       *MediaTranscoderService
	cache            *redis.Client
	maxSizeBytes     int64
	allowedMimeTypes map[string]struct{}
	presignTTL       time.Duration
	playbackTTL      time.Duration
	clock            func() time.Time
}

type NewMediaUploadServiceInput struct {
	MediaRepo         repository.MediaRepository
	Storage           *StorageService
	Transcoder        *MediaTranscoderService
	Cache             *redis.Client
	MaxSizeBytes      int64
	AllowedMimeTypes  []string
	PresignURLTTL     time.Duration
	PlaybackSignedTTL time.Duration
}

func NewMediaUploadService(in NewMediaUploadServiceInput) *MediaUploadService {
	maxSize := in.MaxSizeBytes
	if maxSize <= 0 {
		maxSize = 10 * 1024 * 1024 * 1024
	}
	ttl := in.PresignURLTTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	playbackTTL := in.PlaybackSignedTTL
	if playbackTTL <= 0 {
		playbackTTL = 3 * time.Hour
	}
	allowed := map[string]struct{}{}
	for _, mt := range in.AllowedMimeTypes {
		normalized := strings.TrimSpace(strings.ToLower(mt))
		if normalized == "" {
			continue
		}
		allowed[normalized] = struct{}{}
	}
	if len(allowed) == 0 {
		allowed["video/mp4"] = struct{}{}
		allowed["video/webm"] = struct{}{}
		allowed["video/quicktime"] = struct{}{}
		allowed["video/x-msvideo"] = struct{}{}
		allowed["video/matroska"] = struct{}{}
		allowed["video/x-matroska"] = struct{}{}
	}

	return &MediaUploadService{
		mediaRepo:        in.MediaRepo,
		storage:          in.Storage,
		transcoder:       in.Transcoder,
		cache:            in.Cache,
		maxSizeBytes:     maxSize,
		allowedMimeTypes: allowed,
		presignTTL:       ttl,
		playbackTTL:      playbackTTL,
		clock:            time.Now,
	}
}

type InitUploadInput struct {
	OwnerUserID string
	FileName    string
	ContentType string
	SizeBytes   int64
}

type InitUploadOutput struct {
	MediaID    string
	StorageKey string
	UploadURL  string
}

type CompleteUploadInput struct {
	OwnerUserID string
	MediaID     string
}

type CompleteUploadOutput struct {
	MediaID string
	Status  repository.MediaStatus
}

type PlaybackOutput struct {
	MediaID     string
	Status      repository.MediaStatus
	Manifest    string
	ManifestURL *string
	PreviewURL  *string
	ExpiresAt   time.Time
}

type playbackCacheRecord struct {
	MediaID    string  `json:"mediaId"`
	Status     string  `json:"status"`
	Manifest   string  `json:"manifest"`
	PreviewURL *string `json:"previewUrl,omitempty"`
	ExpiresAt  string  `json:"expiresAt"`
}

func (s *MediaUploadService) ListByOwner(ctx context.Context, ownerUserID string) ([]repository.Media, error) {
	if strings.TrimSpace(ownerUserID) == "" {
		return nil, ErrInvalidUploadInput
	}
	items, err := s.mediaRepo.ListByOwner(ctx, ownerUserID)
	if err != nil {
		return nil, err
	}

	for i := range items {
		if items[i].PreviewURL == nil {
			continue
		}

		previewKey := path.Join("users", items[i].OwnerUserID, "media", items[i].ID, "preview", "preview.jpg")
		signedURL, signErr := s.storage.PresignGetObject(ctx, previewKey, s.presignTTL)
		if signErr != nil {
			continue
		}
		items[i].PreviewURL = &signedURL
	}

	return items, nil
}

func (s *MediaUploadService) InitUpload(ctx context.Context, in InitUploadInput) (InitUploadOutput, error) {
	if strings.TrimSpace(in.OwnerUserID) == "" || strings.TrimSpace(in.FileName) == "" {
		return InitUploadOutput{}, ErrInvalidUploadInput
	}
	if in.SizeBytes <= 0 || in.SizeBytes > s.maxSizeBytes {
		return InitUploadOutput{}, ErrInvalidUploadInput
	}
	if !s.isAllowedMime(in.ContentType) {
		return InitUploadOutput{}, ErrInvalidUploadInput
	}

	mediaID, err := newMediaID()
	if err != nil {
		return InitUploadOutput{}, err
	}

	safeName := sanitizeFilename(in.FileName)
	storageKey := path.Join("users", in.OwnerUserID, "media", mediaID, "original", safeName)

	uploadURL, err := s.storage.PresignPutObject(ctx, storageKey, in.ContentType, s.presignTTL)
	if err != nil {
		return InitUploadOutput{}, err
	}

	created := s.clock()
	media := repository.Media{
		ID:            mediaID,
		OwnerUserID:   in.OwnerUserID,
		Title:         buildTitleFromFilename(safeName),
		OriginalName:  in.FileName,
		StorageKey:    storageKey,
		PlaybackURL:   s.storage.generateObjectURL(storageKey),
		FileSizeBytes: in.SizeBytes,
		MimeType:      strings.ToLower(strings.TrimSpace(in.ContentType)),
		Status:        repository.MediaUploading,
		CreatedAt:     created,
	}
	if _, err := s.mediaRepo.Create(ctx, media); err != nil {
		return InitUploadOutput{}, err
	}

	return InitUploadOutput{MediaID: mediaID, StorageKey: storageKey, UploadURL: uploadURL}, nil
}

func (s *MediaUploadService) CompleteUpload(ctx context.Context, in CompleteUploadInput) (CompleteUploadOutput, error) {
	if strings.TrimSpace(in.OwnerUserID) == "" || strings.TrimSpace(in.MediaID) == "" {
		return CompleteUploadOutput{}, ErrInvalidUploadInput
	}

	media, err := s.mediaRepo.GetByID(ctx, in.MediaID)
	if err != nil {
		return CompleteUploadOutput{}, err
	}
	if media.OwnerUserID != in.OwnerUserID {
		return CompleteUploadOutput{}, ErrForbiddenMedia
	}

	head, err := s.storage.HeadObject(ctx, media.StorageKey)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return CompleteUploadOutput{}, ErrUploadedObjectNotFound
		}
		return CompleteUploadOutput{}, err
	}
	if head.ContentLength <= 0 {
		return CompleteUploadOutput{}, ErrInvalidUploadInput
	}
	if !s.isAllowedMime(head.ContentType) {
		return CompleteUploadOutput{}, ErrInvalidUploadInput
	}

	if err := s.mediaRepo.UpdateUploadState(ctx, media.ID, repository.MediaUploaded, head.ContentLength, normalizeMime(head.ContentType)); err != nil {
		return CompleteUploadOutput{}, err
	}

	if s.transcoder == nil {
		return CompleteUploadOutput{MediaID: media.ID, Status: repository.MediaUploaded}, nil
	}

	if err := s.mediaRepo.UpdateStatus(ctx, media.ID, repository.MediaProcessing); err != nil {
		return CompleteUploadOutput{}, err
	}
	if err := s.transcoder.Enqueue(media.ID); err != nil {
		_ = s.mediaRepo.UpdateStatus(context.Background(), media.ID, repository.MediaFailed)
		return CompleteUploadOutput{}, err
	}

	return CompleteUploadOutput{MediaID: media.ID, Status: repository.MediaProcessing}, nil
}

func (s *MediaUploadService) GetPlayback(ctx context.Context, ownerUserID, mediaID string) (PlaybackOutput, error) {
	if strings.TrimSpace(ownerUserID) == "" || strings.TrimSpace(mediaID) == "" {
		return PlaybackOutput{}, ErrInvalidUploadInput
	}

	cacheKey := s.playbackCacheKey(mediaID)
	if s.cache != nil {
		cached, err := s.cache.Get(ctx, cacheKey).Result()
		if err == nil && strings.TrimSpace(cached) != "" {
			var record playbackCacheRecord
			if unmarshalErr := json.Unmarshal([]byte(cached), &record); unmarshalErr == nil {
				expiresAt, parseErr := time.Parse(time.RFC3339, record.ExpiresAt)
				if parseErr == nil {
					manifestURL := s.buildManifestProxyURL(ctx, record.MediaID)
					return PlaybackOutput{
						MediaID:     record.MediaID,
						Status:      repository.MediaStatus(record.Status),
						Manifest:    record.Manifest,
						ManifestURL: manifestURL,
						PreviewURL:  record.PreviewURL,
						ExpiresAt:   expiresAt,
					}, nil
				}
			}
		}
	}

	media, err := s.mediaRepo.GetByID(ctx, mediaID)
	if err != nil {
		return PlaybackOutput{}, err
	}
	if media.OwnerUserID != ownerUserID {
		return PlaybackOutput{}, ErrForbiddenMedia
	}
	out, err := s.buildPlayback(ctx, media)
	if err != nil {
		return PlaybackOutput{}, err
	}

	if s.cache != nil {
		record := playbackCacheRecord{
			MediaID:    out.MediaID,
			Status:     string(out.Status),
			Manifest:   out.Manifest,
			PreviewURL: out.PreviewURL,
			ExpiresAt:  out.ExpiresAt.UTC().Format(time.RFC3339),
		}
		if payload, marshalErr := json.Marshal(record); marshalErr == nil {
			_ = s.cache.Set(ctx, cacheKey, payload, s.playbackTTL).Err()
		}
	}
	out.ManifestURL = s.buildManifestProxyURL(ctx, out.MediaID)
	return out, nil
}

func (s *MediaUploadService) GetPlaybackByMediaID(ctx context.Context, mediaID string) (PlaybackOutput, error) {
	if strings.TrimSpace(mediaID) == "" {
		return PlaybackOutput{}, ErrInvalidUploadInput
	}

	cacheKey := s.playbackCacheKey(mediaID)
	if s.cache != nil {
		cached, err := s.cache.Get(ctx, cacheKey).Result()
		if err == nil && strings.TrimSpace(cached) != "" {
			var record playbackCacheRecord
			if unmarshalErr := json.Unmarshal([]byte(cached), &record); unmarshalErr == nil {
				expiresAt, parseErr := time.Parse(time.RFC3339, record.ExpiresAt)
				if parseErr == nil {
					manifestURL := s.buildManifestProxyURL(ctx, record.MediaID)
					return PlaybackOutput{
						MediaID:     record.MediaID,
						Status:      repository.MediaStatus(record.Status),
						Manifest:    record.Manifest,
						ManifestURL: manifestURL,
						PreviewURL:  record.PreviewURL,
						ExpiresAt:   expiresAt,
					}, nil
				}
			}
		}
	}

	media, err := s.mediaRepo.GetByID(ctx, mediaID)
	if err != nil {
		return PlaybackOutput{}, err
	}

	out, err := s.buildPlayback(ctx, media)
	if err != nil {
		return PlaybackOutput{}, err
	}

	if s.cache != nil {
		record := playbackCacheRecord{
			MediaID:    out.MediaID,
			Status:     string(out.Status),
			Manifest:   out.Manifest,
			PreviewURL: out.PreviewURL,
			ExpiresAt:  out.ExpiresAt.UTC().Format(time.RFC3339),
		}
		if payload, marshalErr := json.Marshal(record); marshalErr == nil {
			_ = s.cache.Set(ctx, cacheKey, payload, s.playbackTTL).Err()
		}
	}
	out.ManifestURL = s.buildManifestProxyURL(ctx, out.MediaID)
	return out, nil
}

func (s *MediaUploadService) ResolvePlaybackManifest(ctx context.Context, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", ErrInvalidManifestKey
	}
	if s.cache == nil {
		return "", ErrInvalidManifestKey
	}

	mediaID, err := s.cache.Get(ctx, s.playbackManifestTokenKey(token)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", ErrInvalidManifestKey
		}
		return "", err
	}

	media, err := s.mediaRepo.GetByID(ctx, mediaID)
	if err != nil {
		return "", err
	}

	out, err := s.buildPlayback(ctx, media)
	if err != nil {
		return "", err
	}

	return out.Manifest, nil
}

func (s *MediaUploadService) DeleteMedia(ctx context.Context, ownerUserID, mediaID string) error {
	if strings.TrimSpace(ownerUserID) == "" || strings.TrimSpace(mediaID) == "" {
		return ErrInvalidUploadInput
	}

	media, err := s.mediaRepo.GetByID(ctx, mediaID)
	if err != nil {
		return err
	}
	if media.OwnerUserID != ownerUserID {
		return ErrForbiddenMedia
	}

	mediaPrefix := path.Join("users", media.OwnerUserID, "media", media.ID) + "/"
	if err := s.storage.DeleteObjectsByPrefix(ctx, mediaPrefix); err != nil {
		return fmt.Errorf("%w: %v", ErrStorageDelete, err)
	}

	if err := s.mediaRepo.SoftDelete(ctx, media.ID, s.clock()); err != nil {
		return err
	}

	if s.cache != nil {
		_ = s.cache.Del(ctx, s.playbackCacheKey(mediaID)).Err()
	}

	return nil
}

func (s *MediaUploadService) playbackCacheKey(mediaID string) string {
	return "media:playback:v2:" + mediaID
}

func (s *MediaUploadService) playbackManifestTokenKey(token string) string {
	return "media:playback:manifest:v1:" + token
}

func (s *MediaUploadService) buildManifestProxyURL(ctx context.Context, mediaID string) *string {
	if s.cache == nil {
		return nil
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil
	}
	token := hex.EncodeToString(tokenBytes)
	if err := s.cache.Set(ctx, s.playbackManifestTokenKey(token), mediaID, s.playbackTTL).Err(); err != nil {
		return nil
	}
	url := path.Join("/media/playback", token, "index.m3u8")
	return &url
}

func (s *MediaUploadService) buildPlayback(ctx context.Context, media repository.Media) (PlaybackOutput, error) {
	if media.Status != repository.MediaReady {
		return PlaybackOutput{}, ErrMediaNotReady
	}

	manifestKey := path.Join("users", media.OwnerUserID, "media", media.ID, "hls", "index.m3u8")
	manifestBytes, err := s.storage.GetObjectBytes(ctx, manifestKey)
	if err != nil {
		return PlaybackOutput{}, err
	}

	signedManifest, err := s.signManifest(ctx, path.Dir(manifestKey), string(manifestBytes), s.playbackTTL)
	if err != nil {
		return PlaybackOutput{}, err
	}

	var previewURL *string
	previewKey := path.Join("users", media.OwnerUserID, "media", media.ID, "preview", "preview.jpg")
	if signedPreviewURL, signErr := s.storage.PresignGetObject(ctx, previewKey, s.playbackTTL); signErr == nil {
		previewURL = &signedPreviewURL
	}

	expiresAt := s.clock().Add(s.playbackTTL)
	return PlaybackOutput{
		MediaID:    media.ID,
		Status:     media.Status,
		Manifest:   signedManifest,
		PreviewURL: previewURL,
		ExpiresAt:  expiresAt,
	}, nil
}

func (s *MediaUploadService) signManifest(ctx context.Context, baseDirKey, manifest string, ttl time.Duration) (string, error) {
	lines := strings.Split(manifest, "\n")
	signedLines := make([]string, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			signedLines = append(signedLines, rawLine)
			continue
		}

		if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
			signedLines = append(signedLines, rawLine)
			continue
		}

		segmentKey := path.Join(baseDirKey, line)
		signedURL, err := s.storage.PresignGetObject(ctx, segmentKey, ttl)
		if err != nil {
			return "", err
		}
		signedLines = append(signedLines, signedURL)
	}
	return strings.Join(signedLines, "\n"), nil
}

func (s *MediaUploadService) isAllowedMime(mime string) bool {
	_, ok := s.allowedMimeTypes[normalizeMime(mime)]
	return ok
}

func normalizeMime(mime string) string {
	normalized := strings.ToLower(strings.TrimSpace(mime))
	if idx := strings.Index(normalized, ";"); idx >= 0 {
		normalized = strings.TrimSpace(normalized[:idx])
	}
	return normalized
}

func sanitizeFilename(name string) string {
	safeName := filepath.Base(strings.TrimSpace(name))
	if safeName == "" || safeName == "." || safeName == "/" {
		return "file"
	}
	safeName = strings.ReplaceAll(safeName, " ", "_")
	return safeName
}

func buildTitleFromFilename(fileName string) string {
	ext := filepath.Ext(fileName)
	title := strings.TrimSpace(strings.TrimSuffix(fileName, ext))
	if title == "" {
		return "media"
	}
	return title
}

func newMediaID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate media id: %w", err)
	}
	return "media_" + hex.EncodeToString(b), nil
}
