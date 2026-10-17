package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"spb-rent/backend/internal/buildings"
)

type BuildingReader interface {
	GetForPost(context.Context, int64, int) (*buildings.Info, error)
}
type BuildingHandler struct{ reader BuildingReader }

func NewBuildingHandler(reader BuildingReader) *BuildingHandler {
	return &BuildingHandler{reader: reader}
}

func (h *BuildingHandler) Get(w http.ResponseWriter, r *http.Request) {
	channelID, err := strconv.ParseInt(chi.URLParam(r, "channelID"), 10, 64)
	if err != nil || channelID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_channel_id"})
		return
	}
	messageID64, err := strconv.ParseInt(chi.URLParam(r, "messageID"), 10, 32)
	if err != nil || messageID64 <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_message_id"})
		return
	}
	info, err := h.reader.GetForPost(r.Context(), channelID, int(messageID64))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "building_lookup_failed"})
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func RegisterBuildingRoutes(r chi.Router, apiKey string, reader BuildingReader) {
	handler := NewBuildingHandler(reader)
	r.With(func(next http.Handler) http.Handler {
		return telegramAPIKeyMiddleware(apiKey, next)
	}).Get("/telegram/building/{channelID}/{messageID}", handler.Get)
}
