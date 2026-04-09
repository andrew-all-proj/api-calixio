package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MediaStatus string

const (
	MediaUploading  MediaStatus = "uploading"
	MediaUploaded   MediaStatus = "uploaded"
	MediaProcessing MediaStatus = "processing"
	MediaReady      MediaStatus = "ready"
	MediaFailed     MediaStatus = "failed"
)

type Media struct {
	ID            string
	OwnerUserID   string
	Title         string
	OriginalName  string
	StorageKey    string
	PlaybackURL   string
	PreviewURL    *string
	DurationSec   *int
	FileSizeBytes int64
	MimeType      string
	Status        MediaStatus
	CreatedAt     time.Time
	DeletedAt     *time.Time
}

type MediaRepository interface {
	Create(ctx context.Context, media Media) (Media, error)
	ListByOwner(ctx context.Context, ownerUserID string) ([]Media, error)
	GetByID(ctx context.Context, id string) (Media, error)
	ListExistingIDs(ctx context.Context, ids []string) ([]string, error)
	UpdateUploadState(ctx context.Context, id string, status MediaStatus, fileSizeBytes int64, mimeType string) error
	UpdateStatus(ctx context.Context, id string, status MediaStatus) error
	UpdateTranscodeResult(ctx context.Context, id, playbackURL string, previewURL *string, durationSec *int, status MediaStatus) error
	SoftDelete(ctx context.Context, id string, deletedAt time.Time) error
}

type PostgresMediaRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresMediaRepository(pool *pgxpool.Pool) *PostgresMediaRepository {
	return &PostgresMediaRepository{pool: pool}
}

func (r *PostgresMediaRepository) Create(ctx context.Context, media Media) (Media, error) {
	query := `
		INSERT INTO media (
			id, owner_user_id, title, original_name, storage_key, playback_url, preview_url,
			duration_sec, file_size_bytes, mime_type, status, created_at, deleted_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, owner_user_id, title, original_name, storage_key, playback_url, preview_url,
			duration_sec, file_size_bytes, mime_type, status, created_at, deleted_at
	`
	row := r.pool.QueryRow(
		ctx,
		query,
		media.ID,
		media.OwnerUserID,
		media.Title,
		media.OriginalName,
		media.StorageKey,
		media.PlaybackURL,
		media.PreviewURL,
		media.DurationSec,
		media.FileSizeBytes,
		media.MimeType,
		string(media.Status),
		media.CreatedAt,
		media.DeletedAt,
	)

	var out Media
	var status string
	if err := row.Scan(
		&out.ID,
		&out.OwnerUserID,
		&out.Title,
		&out.OriginalName,
		&out.StorageKey,
		&out.PlaybackURL,
		&out.PreviewURL,
		&out.DurationSec,
		&out.FileSizeBytes,
		&out.MimeType,
		&status,
		&out.CreatedAt,
		&out.DeletedAt,
	); err != nil {
		return Media{}, err
	}
	out.Status = MediaStatus(status)
	return out, nil
}

func (r *PostgresMediaRepository) ListByOwner(ctx context.Context, ownerUserID string) ([]Media, error) {
	query := `
		SELECT id, owner_user_id, title, original_name, storage_key, playback_url, preview_url,
			duration_sec, file_size_bytes, mime_type, status, created_at, deleted_at
		FROM media
		WHERE owner_user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]Media, 0)
	for rows.Next() {
		var out Media
		var status string
		if err := rows.Scan(
			&out.ID,
			&out.OwnerUserID,
			&out.Title,
			&out.OriginalName,
			&out.StorageKey,
			&out.PlaybackURL,
			&out.PreviewURL,
			&out.DurationSec,
			&out.FileSizeBytes,
			&out.MimeType,
			&status,
			&out.CreatedAt,
			&out.DeletedAt,
		); err != nil {
			return nil, err
		}
		out.Status = MediaStatus(status)
		items = append(items, out)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresMediaRepository) GetByID(ctx context.Context, id string) (Media, error) {
	query := `
		SELECT id, owner_user_id, title, original_name, storage_key, playback_url, preview_url,
			duration_sec, file_size_bytes, mime_type, status, created_at, deleted_at
		FROM media
		WHERE id = $1 AND deleted_at IS NULL
	`
	row := r.pool.QueryRow(ctx, query, id)

	var out Media
	var status string
	if err := row.Scan(
		&out.ID,
		&out.OwnerUserID,
		&out.Title,
		&out.OriginalName,
		&out.StorageKey,
		&out.PlaybackURL,
		&out.PreviewURL,
		&out.DurationSec,
		&out.FileSizeBytes,
		&out.MimeType,
		&status,
		&out.CreatedAt,
		&out.DeletedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Media{}, ErrNotFound
		}
		return Media{}, err
	}
	out.Status = MediaStatus(status)
	return out, nil
}

func (r *PostgresMediaRepository) ListExistingIDs(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	query := `
		SELECT id
		FROM media
		WHERE deleted_at IS NULL
		  AND id = ANY($1)
	`

	rows, err := r.pool.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	existing := make([]string, 0, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		existing = append(existing, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return existing, nil
}

func (r *PostgresMediaRepository) UpdateUploadState(ctx context.Context, id string, status MediaStatus, fileSizeBytes int64, mimeType string) error {
	query := `
		UPDATE media
		SET status = $2, file_size_bytes = $3, mime_type = $4
		WHERE id = $1 AND deleted_at IS NULL
	`
	ct, err := r.pool.Exec(ctx, query, id, string(status), fileSizeBytes, mimeType)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresMediaRepository) UpdateStatus(ctx context.Context, id string, status MediaStatus) error {
	query := `
		UPDATE media
		SET status = $2
		WHERE id = $1 AND deleted_at IS NULL
	`
	ct, err := r.pool.Exec(ctx, query, id, string(status))
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresMediaRepository) UpdateTranscodeResult(ctx context.Context, id, playbackURL string, previewURL *string, durationSec *int, status MediaStatus) error {
	query := `
		UPDATE media
		SET status = $2,
			playback_url = $3,
			preview_url = $4,
			duration_sec = $5,
			mime_type = 'application/vnd.apple.mpegurl'
		WHERE id = $1 AND deleted_at IS NULL
	`
	ct, err := r.pool.Exec(ctx, query, id, string(status), playbackURL, previewURL, durationSec)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresMediaRepository) SoftDelete(ctx context.Context, id string, deletedAt time.Time) error {
	query := `
		UPDATE media
		SET deleted_at = $2
		WHERE id = $1 AND deleted_at IS NULL
	`
	ct, err := r.pool.Exec(ctx, query, id, deletedAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
