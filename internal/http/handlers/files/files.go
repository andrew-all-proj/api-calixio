package files

import (
	"errors"
	"net/http"

	"calixio/internal/http/authn"
	"calixio/internal/http/dto"
	httputil "calixio/internal/http/httputil"
	"calixio/internal/repository"
	"calixio/internal/service"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type Handler struct {
	media  *service.MediaUploadService
	logger *zap.Logger
}

func NewHandler(media *service.MediaUploadService, logger *zap.Logger) *Handler {
	return &Handler{media: media, logger: logger}
}

func (h *Handler) ListMedia(w http.ResponseWriter, r *http.Request) {
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	items, err := h.media.ListByOwner(r.Context(), userID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidUploadInput):
			httputil.RespondError(w, http.StatusBadRequest, "invalid_upload_input")
		default:
			h.logger.Error("list media", zap.Error(err), zap.String("user_id", userID))
			httputil.RespondError(w, http.StatusInternalServerError, "media_list_failed")
		}
		return
	}

	resp := make([]dto.MediaListItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, dto.MediaListItemResponse{
			ID:            item.ID,
			Title:         item.Title,
			OriginalName:  item.OriginalName,
			PlaybackURL:   item.PlaybackURL,
			PreviewURL:    item.PreviewURL,
			DurationSec:   item.DurationSec,
			FileSizeBytes: item.FileSizeBytes,
			MimeType:      item.MimeType,
			Status:        string(item.Status),
			CreatedAt:     item.CreatedAt.Format(httputil.TimeLayout),
		})
	}

	httputil.RespondJSON(w, http.StatusOK, resp)
}

func (h *Handler) InitMediaUpload(w http.ResponseWriter, r *http.Request) {
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req dto.InitMediaUploadRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := httputil.ValidateStruct(req); err != nil {
		httputil.RespondValidationError(w, err)
		return
	}

	out, err := h.media.InitUpload(r.Context(), service.InitUploadInput{
		OwnerUserID: userID,
		FileName:    req.FileName,
		ContentType: req.ContentType,
		SizeBytes:   req.SizeBytes,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidUploadInput):
			httputil.RespondError(w, http.StatusBadRequest, "invalid_upload_input")
		default:
			h.logger.Error("init media upload", zap.Error(err), zap.String("user_id", userID))
			httputil.RespondError(w, http.StatusInternalServerError, "media_upload_init_failed")
		}
		return
	}

	httputil.RespondJSON(w, http.StatusCreated, dto.InitMediaUploadResponse{
		MediaID:    out.MediaID,
		UploadURL:  out.UploadURL,
		StorageKey: out.StorageKey,
	})
}

func (h *Handler) CompleteMediaUpload(w http.ResponseWriter, r *http.Request) {
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req dto.CompleteMediaUploadRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := httputil.ValidateStruct(req); err != nil {
		httputil.RespondValidationError(w, err)
		return
	}

	out, err := h.media.CompleteUpload(r.Context(), service.CompleteUploadInput{
		OwnerUserID: userID,
		MediaID:     req.MediaID,
	})
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			httputil.RespondError(w, http.StatusNotFound, "media_not_found")
		case errors.Is(err, service.ErrForbiddenMedia):
			httputil.RespondError(w, http.StatusForbidden, "media_forbidden")
		case errors.Is(err, service.ErrInvalidUploadInput):
			httputil.RespondError(w, http.StatusBadRequest, "invalid_uploaded_object")
		default:
			h.logger.Error("complete media upload", zap.Error(err), zap.String("user_id", userID), zap.String("media_id", req.MediaID))
			httputil.RespondError(w, http.StatusInternalServerError, "media_upload_complete_failed")
		}
		return
	}

	httputil.RespondJSON(w, http.StatusOK, dto.CompleteMediaUploadResponse{
		MediaID: out.MediaID,
		Status:  string(out.Status),
	})
}

func (h *Handler) GetPlayback(w http.ResponseWriter, r *http.Request) {
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	mediaID := chi.URLParam(r, "id")
	if mediaID == "" {
		httputil.RespondError(w, http.StatusBadRequest, "media_id_required")
		return
	}

	out, err := h.media.GetPlayback(r.Context(), userID, mediaID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			httputil.RespondError(w, http.StatusNotFound, "media_not_found")
		case errors.Is(err, service.ErrForbiddenMedia):
			httputil.RespondError(w, http.StatusForbidden, "media_forbidden")
		case errors.Is(err, service.ErrMediaNotReady):
			httputil.RespondError(w, http.StatusConflict, "media_not_ready")
		case errors.Is(err, service.ErrInvalidUploadInput):
			httputil.RespondError(w, http.StatusBadRequest, "invalid_playback_request")
		default:
			h.logger.Error("media playback", zap.Error(err), zap.String("user_id", userID), zap.String("media_id", mediaID))
			httputil.RespondError(w, http.StatusInternalServerError, "media_playback_failed")
		}
		return
	}

	httputil.RespondJSON(w, http.StatusOK, dto.PlaybackMediaResponse{
		MediaID:     out.MediaID,
		Status:      string(out.Status),
		Manifest:    out.Manifest,
		ManifestURL: out.ManifestURL,
		PreviewURL:  out.PreviewURL,
		ExpiresAt:   out.ExpiresAt.UTC().Format(httputil.TimeLayout),
	})
}

func (h *Handler) GetPlaybackManifest(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		httputil.RespondError(w, http.StatusBadRequest, "playback_token_required")
		return
	}

	manifest, err := h.media.ResolvePlaybackManifest(r.Context(), token)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidManifestKey), errors.Is(err, repository.ErrNotFound), errors.Is(err, service.ErrMediaNotReady):
			httputil.RespondError(w, http.StatusNotFound, "media_playback_not_found")
		default:
			h.logger.Error("resolve media playback manifest", zap.Error(err), zap.String("token", token))
			httputil.RespondError(w, http.StatusInternalServerError, "media_playback_failed")
		}
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(manifest))
}

func (h *Handler) DeleteMedia(w http.ResponseWriter, r *http.Request) {
	userID := authn.UserIDFromContext(r.Context())
	if userID == "" {
		httputil.RespondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	mediaID := chi.URLParam(r, "id")
	if mediaID == "" {
		httputil.RespondError(w, http.StatusBadRequest, "media_id_required")
		return
	}

	if err := h.media.DeleteMedia(r.Context(), userID, mediaID); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			httputil.RespondError(w, http.StatusNotFound, "media_not_found")
		case errors.Is(err, service.ErrForbiddenMedia):
			httputil.RespondError(w, http.StatusForbidden, "media_forbidden")
		case errors.Is(err, service.ErrInvalidUploadInput):
			httputil.RespondError(w, http.StatusBadRequest, "invalid_media_delete_request")
		default:
			h.logger.Error("delete media", zap.Error(err), zap.String("user_id", userID), zap.String("media_id", mediaID))
			httputil.RespondError(w, http.StatusInternalServerError, "media_delete_failed")
		}
		return
	}

	httputil.RespondJSON(w, http.StatusOK, dto.DeleteMediaResponse{
		MediaID: mediaID,
		Status:  "deleted",
	})
}
