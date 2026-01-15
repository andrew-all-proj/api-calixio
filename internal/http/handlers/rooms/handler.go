package rooms

import (
	"errors"
	"net/http"

	"calixio/internal/http/authn"
	"calixio/internal/http/dto"
	httputil "calixio/internal/http/httputil"
	"calixio/internal/repository"
	"calixio/internal/service"

	"go.uber.org/zap"
)

type Handler struct {
	rooms  *service.RoomService
	jwt    *authn.JWTService
	logger *zap.Logger
}

func NewHandler(rooms *service.RoomService, jwt *authn.JWTService, logger *zap.Logger) *Handler {
	return &Handler{rooms: rooms, jwt: jwt, logger: logger}
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

	room, err := h.rooms.CreateRoom(r.Context(), service.CreateRoomInput{Name: req.Name, CreatedBy: userID, UserID: userID})
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
	userID := authn.UserIDFromContext(r.Context())
	roomID := httputil.ChiParam(r, "id")
	if roomID == "" {
		httputil.RespondError(w, http.StatusBadRequest, "room_id_required")
		return
	}

	jwt, room, err := h.rooms.JoinRoom(r.Context(), roomID, userID)
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
	})
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
