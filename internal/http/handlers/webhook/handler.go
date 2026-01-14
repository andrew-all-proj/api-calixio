package webhook

import (
	"net/http"

	httputil "calixio/internal/http/httputil"
	"calixio/internal/livekit"
	"calixio/internal/service"

	"go.uber.org/zap"
)

type Handler struct {
	webhook *service.WebhookService
	lk      *livekit.Client
	logger  *zap.Logger
}

func NewHandler(webhook *service.WebhookService, lk *livekit.Client, logger *zap.Logger) *Handler {
	return &Handler{webhook: webhook, lk: lk, logger: logger}
}

func (h *Handler) LiveKitWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := h.lk.ReceiveWebhook(r)
	if err != nil {
		h.logger.Warn("webhook verify failed", zap.Error(err))
		httputil.RespondError(w, http.StatusUnauthorized, "invalid_webhook")
		return
	}

	if err := h.webhook.HandleEvent(r.Context(), payload); err != nil {
		h.logger.Error("webhook handle", zap.Error(err))
		httputil.RespondError(w, http.StatusInternalServerError, "webhook_handle_failed")
		return
	}
	httputil.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
