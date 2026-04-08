package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Room struct {
	ID          string
	Name        string
	OwnerUserID string
	MediaID     *string
	Status      RoomStatus
	CreatedAt   time.Time
	EndedAt     *time.Time
}

type RoomStatus string

const (
	RoomPending RoomStatus = "pending"
	RoomActive  RoomStatus = "active"
	RoomEnded   RoomStatus = "ended"
)

type RoomRepository interface {
	CreateRoom(ctx context.Context, room Room) (Room, error)
	ListRoomsByOwner(ctx context.Context, ownerUserID string) ([]Room, error)
	ListActiveRooms(ctx context.Context) ([]Room, error)
	GetRoomByID(ctx context.Context, id string) (Room, error)
	UpdateRoomMedia(ctx context.Context, id string, mediaID *string) (Room, error)
	EndRoom(ctx context.Context, id string, endedAt time.Time) error
}

type PostgresRoomRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRoomRepository(pool *pgxpool.Pool) *PostgresRoomRepository {
	return &PostgresRoomRepository{pool: pool}
}

func (r *PostgresRoomRepository) CreateRoom(ctx context.Context, room Room) (Room, error) {
	query := `
		INSERT INTO rooms (id, name, owner_user_id, media_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, owner_user_id, media_id, status, created_at, ended_at
	`
	row := r.pool.QueryRow(ctx, query, room.ID, room.Name, room.OwnerUserID, room.MediaID, string(room.Status), room.CreatedAt)

	var out Room
	var status string
	err := row.Scan(&out.ID, &out.Name, &out.OwnerUserID, &out.MediaID, &status, &out.CreatedAt, &out.EndedAt)
	if err != nil {
		return Room{}, err
	}
	out.Status = RoomStatus(status)
	return out, nil
}

func (r *PostgresRoomRepository) ListRoomsByOwner(ctx context.Context, ownerUserID string) ([]Room, error) {
	query := `
		SELECT id, name, owner_user_id, media_id, status, created_at, ended_at
		FROM rooms
		WHERE owner_user_id = $1
		ORDER BY created_at DESC
	`
	rows, err := r.pool.Query(ctx, query, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := make([]Room, 0)
	for rows.Next() {
		var out Room
		var status string
		if err := rows.Scan(&out.ID, &out.Name, &out.OwnerUserID, &out.MediaID, &status, &out.CreatedAt, &out.EndedAt); err != nil {
			return nil, err
		}
		out.Status = RoomStatus(status)
		rooms = append(rooms, out)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rooms, nil
}

func (r *PostgresRoomRepository) ListActiveRooms(ctx context.Context) ([]Room, error) {
	query := `
		SELECT id, name, owner_user_id, media_id, status, created_at, ended_at
		FROM rooms
		WHERE status = 'active'
		ORDER BY created_at ASC
	`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := make([]Room, 0)
	for rows.Next() {
		var out Room
		var status string
		if err := rows.Scan(&out.ID, &out.Name, &out.OwnerUserID, &out.MediaID, &status, &out.CreatedAt, &out.EndedAt); err != nil {
			return nil, err
		}
		out.Status = RoomStatus(status)
		rooms = append(rooms, out)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return rooms, nil
}

func (r *PostgresRoomRepository) GetRoomByID(ctx context.Context, id string) (Room, error) {
	query := `
		SELECT id, name, owner_user_id, media_id, status, created_at, ended_at
		FROM rooms
		WHERE id = $1
	`
	row := r.pool.QueryRow(ctx, query, id)
	var out Room
	var status string
	if err := row.Scan(&out.ID, &out.Name, &out.OwnerUserID, &out.MediaID, &status, &out.CreatedAt, &out.EndedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Room{}, ErrNotFound
		}
		return Room{}, err
	}
	out.Status = RoomStatus(status)
	return out, nil
}

func (r *PostgresRoomRepository) EndRoom(ctx context.Context, id string, endedAt time.Time) error {
	query := `
		UPDATE rooms
		SET status = 'ended', ended_at = $2
		WHERE id = $1 AND status = 'active'
	`
	ct, err := r.pool.Exec(ctx, query, id, endedAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *PostgresRoomRepository) UpdateRoomMedia(ctx context.Context, id string, mediaID *string) (Room, error) {
	query := `
		UPDATE rooms
		SET media_id = $2
		WHERE id = $1
		RETURNING id, name, owner_user_id, media_id, status, created_at, ended_at
	`
	row := r.pool.QueryRow(ctx, query, id, mediaID)

	var out Room
	var status string
	if err := row.Scan(&out.ID, &out.Name, &out.OwnerUserID, &out.MediaID, &status, &out.CreatedAt, &out.EndedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Room{}, ErrNotFound
		}
		return Room{}, err
	}
	out.Status = RoomStatus(status)
	return out, nil
}
