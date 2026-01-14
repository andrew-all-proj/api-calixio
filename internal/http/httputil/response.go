package httputil

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
)

const TimeLayout = "2006-01-02T15:04:05Z07:00"

func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func RespondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func RespondError(w http.ResponseWriter, status int, code string) {
	RespondJSON(w, status, map[string]string{"error": code})
}

func RespondValidationError(w http.ResponseWriter, err error) {
	fields := map[string]string{}
	reason := ""
	if verrs, ok := err.(validator.ValidationErrors); ok {
		for _, fe := range verrs {
			field := strings.ToLower(fe.Field())
			tag := fe.Tag()
			fields[field] = tag
			if reason == "" {
				reason = field + "_" + tag
			}
		}
	}
	if len(fields) == 0 {
		RespondError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	RespondJSON(w, http.StatusBadRequest, map[string]any{
		"error":  "validation_failed",
		"reason": reason,
		"fields": fields,
	})
}

func ChiParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
