package rooms

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"calixio/internal/http/authn"
	"calixio/internal/http/dto"
	httputil "calixio/internal/http/httputil"
	"calixio/internal/repository"
	"calixio/internal/service"

	"go.uber.org/zap"
)

type Handler struct {
	rooms    *service.RoomService
	media    *service.MediaUploadService
	playback *service.RoomPlaybackService
	jwt      *authn.JWTService
	logger   *zap.Logger
}

func NewHandler(
	rooms *service.RoomService,
	media *service.MediaUploadService,
	playback *service.RoomPlaybackService,
	jwt *authn.JWTService,
	logger *zap.Logger,
) *Handler {
	return &Handler{rooms: rooms, media: media, playback: playback, jwt: jwt, logger: logger}
}

func (h *Handler) ListRooms(w http.ResponseWriter, r *http.Request) {
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	rooms, err := h.rooms.ListRoomsByOwner(r.Context(), userID)
	if err != nil {
		h.logger.Error("list rooms", zap.Error(err), zap.String("user_id", userID))
		httputil.RespondError(w, http.StatusInternalServerError, "room_list_failed")
		return
	}

	out := make([]dto.RoomResponse, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, dto.RoomResponse{
			ID:        room.ID,
			Name:      room.Name,
			Status:    string(room.Status),
			CreatedAt: room.CreatedAt.Format(httputil.TimeLayout),
		})
	}

	httputil.RespondJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	var req dto.CreateRoomRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := httputil.ValidateStruct(req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	room, err := h.rooms.CreateRoom(r.Context(), service.CreateRoomInput{Name: req.Name, OwnerUserID: userID})
	if err != nil {
		h.logger.Error("create room", zap.Error(err))
		httputil.RespondError(w, http.StatusInternalServerError, "room_create_failed")
		return
	}

	httputil.RespondJSON(w, http.StatusCreated, dto.RoomResponse{
		ID:        room.ID,
		Name:      room.Name,
		Status:    string(room.Status),
		CreatedAt: room.CreatedAt.Format(httputil.TimeLayout),
	})
}

func (h *Handler) JoinRoom(w http.ResponseWriter, r *http.Request) {
	var req dto.JoinRoomRequest
	if err := httputil.DecodeJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		httputil.RespondError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := httputil.ValidateStruct(req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "validation_failed")
		return
	}

	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		guestID, err := newGuestID()
		if err != nil {
			h.logger.Error("guest identity", zap.Error(err))
			httputil.RespondError(w, http.StatusInternalServerError, "guest_identity_failed")
			return
		}
		userID = guestID
	}

	displayName := strings.TrimSpace(req.UserName)
	if displayName == "" {
		displayName = "Гость"
	}
	h.logger.Info("joining room", zap.String("user_name", displayName))
	roomID := httputil.ChiParam(r, "id")
	if roomID == "" {
		httputil.RespondError(w, http.StatusBadRequest, "room_id_required")
		return
	}

	jwt, room, err := h.rooms.JoinRoom(r.Context(), roomID, userID, displayName)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondError(w, http.StatusNotFound, "room_not_found")
			return
		}
		if errors.Is(err, service.ErrRoomEnded) {
			httputil.RespondError(w, http.StatusConflict, "room_ended")
			return
		}
		h.logger.Error("join room", zap.Error(err))
		httputil.RespondError(w, http.StatusInternalServerError, "room_join_failed")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, dto.JoinRoomResponse{
		RoomID:    room.ID,
		RoomName:  room.Name,
		Token:     jwt,
		ExpiresIn: h.jwt.AccessTTLSeconds(),
		State:     h.buildRoomStateResponse(r.Context(), room),
	})
}

func (h *Handler) UpdateRoomState(w http.ResponseWriter, r *http.Request) {
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	roomID := httputil.ChiParam(r, "id")
	if roomID == "" {
		httputil.RespondError(w, http.StatusBadRequest, "room_id_required")
		return
	}

	var req dto.UpdateRoomStateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := httputil.ValidateStruct(req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "validation_failed")
		return
	}

	if req.Mode == "movie" && (req.MediaID == nil || strings.TrimSpace(*req.MediaID) == "") {
		httputil.RespondError(w, http.StatusBadRequest, "media_id_required")
		return
	}

	room, err := h.rooms.UpdateRoomState(r.Context(), roomID, userID, req.Mode, req.MediaID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			httputil.RespondError(w, http.StatusNotFound, "room_not_found")
		case errors.Is(err, service.ErrRoomForbidden):
			httputil.RespondError(w, http.StatusForbidden, "room_forbidden")
		case errors.Is(err, service.ErrRoomEnded):
			httputil.RespondError(w, http.StatusConflict, "room_ended")
		case errors.Is(err, service.ErrMediaRequiredForMovieMode):
			httputil.RespondError(w, http.StatusBadRequest, "media_id_required")
		case errors.Is(err, service.ErrMediaForbiddenForRoom):
			httputil.RespondError(w, http.StatusForbidden, "media_forbidden")
		case errors.Is(err, service.ErrMediaNotReady):
			httputil.RespondError(w, http.StatusConflict, "media_not_ready")
		default:
			h.logger.Error("update room state", zap.Error(err), zap.String("room_id", roomID), zap.String("user_id", userID))
			httputil.RespondError(w, http.StatusInternalServerError, "room_state_update_failed")
		}
		return
	}

	httputil.RespondJSON(w, http.StatusOK, dto.UpdateRoomStateResponse{
		RoomID:   room.ID,
		RoomName: room.Name,
		State:    h.buildRoomStateResponse(r.Context(), room),
	})
}

func (h *Handler) buildRoomStateResponse(ctx context.Context, room repository.Room) dto.RoomStateResponse {
	resp := dto.RoomStateResponse{
		Mode: "conference",
	}
	if room.MediaID == nil || strings.TrimSpace(*room.MediaID) == "" {
		return resp
	}

	resp.Mode = "movie"
	resp.MediaID = room.MediaID
	playback, err := h.media.GetPlaybackByMediaID(ctx, *room.MediaID)
	if err != nil {
		return resp
	}
	resp.Playback = &dto.PlaybackMediaResponse{
		MediaID:     playback.MediaID,
		Status:      string(playback.Status),
		Manifest:    playback.Manifest,
		ManifestURL: playback.ManifestURL,
		PreviewURL:  playback.PreviewURL,
		ExpiresAt:   playback.ExpiresAt.UTC().Format(httputil.TimeLayout),
	}

	return resp
}

func (h *Handler) GetRoomPlaybackState(w http.ResponseWriter, r *http.Request) {
	roomID := httputil.ChiParam(r, "id")
	if roomID == "" {
		httputil.RespondError(w, http.StatusBadRequest, "room_id_required")
		return
	}

	state, err := h.playback.GetState(r.Context(), roomID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrPlaybackStateNotFound):
			httputil.RespondError(w, http.StatusNotFound, "playback_state_not_found")
		case errors.Is(err, repository.ErrNotFound):
			httputil.RespondError(w, http.StatusNotFound, "room_not_found")
		default:
			h.logger.Error("get room playback state", zap.Error(err), zap.String("room_id", roomID))
			httputil.RespondError(w, http.StatusInternalServerError, "playback_state_failed")
		}
		return
	}

	httputil.RespondJSON(w, http.StatusOK, dto.RoomPlaybackStateResponse{
		RoomID:       state.RoomID,
		MediaID:      state.MediaID,
		Status:       string(state.Status),
		PositionMs:   state.PositionMs,
		PlaybackRate: state.PlaybackRate,
		UpdatedAt:    state.UpdatedAt,
		Version:      state.Version,
		HostID:       state.HostID,
	})
}

func (h *Handler) UpdateRoomPlaybackState(w http.ResponseWriter, r *http.Request) {
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	roomID := httputil.ChiParam(r, "id")
	if roomID == "" {
		httputil.RespondError(w, http.StatusBadRequest, "room_id_required")
		return
	}

	var req dto.UpdateRoomPlaybackRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := httputil.ValidateStruct(req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "validation_failed")
		return
	}

	state, err := h.playback.SaveByHost(r.Context(), roomID, userID, service.UpdateRoomPlaybackInput{
		MediaID:      req.MediaID,
		Status:       service.PlaybackStatus(req.Status),
		PositionMs:   req.PositionMs,
		PlaybackRate: req.PlaybackRate,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			httputil.RespondError(w, http.StatusNotFound, "room_not_found")
		case errors.Is(err, service.ErrRoomForbidden):
			httputil.RespondError(w, http.StatusForbidden, "room_forbidden")
		case errors.Is(err, service.ErrRoomEnded):
			httputil.RespondError(w, http.StatusConflict, "room_ended")
		default:
			h.logger.Error("update room playback state", zap.Error(err), zap.String("room_id", roomID), zap.String("user_id", userID))
			httputil.RespondError(w, http.StatusInternalServerError, "playback_state_update_failed")
		}
		return
	}

	httputil.RespondJSON(w, http.StatusOK, dto.RoomPlaybackStateResponse{
		RoomID:       state.RoomID,
		MediaID:      state.MediaID,
		Status:       string(state.Status),
		PositionMs:   state.PositionMs,
		PlaybackRate: state.PlaybackRate,
		UpdatedAt:    state.UpdatedAt,
		Version:      state.Version,
		HostID:       state.HostID,
	})
}

func newGuestID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "guest-" + hex.EncodeToString(b), nil
}

func (h *Handler) EndRoom(w http.ResponseWriter, r *http.Request) {
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	roomID := httputil.ChiParam(r, "id")
	if roomID == "" {
		httputil.RespondError(w, http.StatusBadRequest, "room_id_required")
		return
	}

	room, err := h.rooms.EndRoom(r.Context(), roomID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			httputil.RespondError(w, http.StatusNotFound, "room_not_found")
			return
		}
		h.logger.Error("end room", zap.Error(err))
		httputil.RespondError(w, http.StatusInternalServerError, "room_end_failed")
		return
	}

	httputil.RespondJSON(w, http.StatusOK, dto.RoomResponse{
		ID:        room.ID,
		Name:      room.Name,
		Status:    string(room.Status),
		CreatedAt: room.CreatedAt.Format(httputil.TimeLayout),
	})
}
