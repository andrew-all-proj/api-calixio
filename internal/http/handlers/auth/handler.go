package auth

import (
	"errors"
	"net/http"
	"time"

	"calixio/internal/http/authn"
	"calixio/internal/http/dto"
	httputil "calixio/internal/http/httputil"
	"calixio/internal/repository"
	"calixio/internal/service"

	"go.uber.org/zap"
)

type Handler struct {
	auth   *service.AuthService
	jwt    *authn.JWTService
	logger *zap.Logger
}

const refreshCookieName = "refresh_token"

func NewHandler(auth *service.AuthService, jwt *authn.JWTService, logger *zap.Logger) *Handler {
	return &Handler{auth: auth, jwt: jwt, logger: logger}
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := httputil.ValidateStruct(req); err != nil {
		httputil.RespondValidationError(w, err)
		return
	}

	result, err := h.auth.Login(r.Context(), service.LoginInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		if errors.Is(err, repository.ErrInvalidCredentials) {
			httputil.RespondError(w, http.StatusUnauthorized, "invalid_credentials")
			return
		}
		h.logger.Error("login", zap.Error(err))
		httputil.RespondError(w, http.StatusInternalServerError, "login_failed")
		return
	}

	setRefreshCookie(w, r, result.RefreshToken, result.RefreshTTL)
	httputil.RespondJSON(w, http.StatusOK, dto.LoginResponse{
		Name:         result.Name,
		AccessToken:  result.AccessToken,
		AccessTTL:    int(result.AccessTTL.Seconds()),
		RefreshTTL:   int(result.RefreshTTL.Seconds()),
	})
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req dto.RegisterRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if err := httputil.ValidateStruct(req); err != nil {
		h.logger.Error("register request", zap.Error(err))
		httputil.RespondValidationError(w, err)
		return
	}

	result, err := h.auth.Register(r.Context(), service.RegisterInput{
		Name:     req.Name,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			httputil.RespondError(w, http.StatusConflict, "email_exists")
			return
		}
		h.logger.Error("register", zap.Error(err))
		httputil.RespondError(w, http.StatusInternalServerError, "register_failed")
		return
	}

	httputil.RespondJSON(w, http.StatusCreated, dto.RegisterResponse{UserID: result.UserID})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil {
		httputil.RespondError(w, http.StatusBadRequest, "refresh_token_required")
		return
	}

	result, err := h.auth.Refresh(r.Context(), cookie.Value)
	if err != nil {
		if errors.Is(err, repository.ErrSessionNotFound) {
			httputil.RespondError(w, http.StatusUnauthorized, "invalid_refresh")
			return
		}
		h.logger.Error("refresh", zap.Error(err))
		httputil.RespondError(w, http.StatusInternalServerError, "refresh_failed")
		return
	}

	setRefreshCookie(w, r, result.RefreshToken, result.RefreshTTL)
	httputil.RespondJSON(w, http.StatusOK, dto.RefreshResponse{
		UserID:       result.UserID,
		AccessToken:  result.AccessToken,
		AccessTTL:    int(result.AccessTTL.Seconds()),
		RefreshTTL:   int(result.RefreshTTL.Seconds()),
		Version:      result.Version,
	})
}

func setRefreshCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	cookie := &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		HttpOnly: true,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		Expires:  time.Now().Add(ttl),
		SameSite: http.SameSiteNoneMode,
	}
	if r.TLS != nil {
		cookie.Secure = true
	} else {
		cookie.SameSite = http.SameSiteLaxMode
	}
	http.SetCookie(w, cookie)
}
