package inventory

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// Handler atiende la API HTTP de Inventory
type Handler struct {
	service *Service
}

// NewHandler crea las rutas HTTP de Inventory
func NewHandler(service *Service) http.Handler {
	handler := &Handler{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("POST /inventory/reserve", handler.reserve)
	mux.HandleFunc("POST /inventory/release", handler.release)
	mux.HandleFunc("GET /inventory/{sku}", handler.getItem)
	return mux
}

func (h *Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) reserve(writer http.ResponseWriter, request *http.Request) {
	var input ReserveRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	result, err := h.service.Reserve(request.Context(), input)
	if err != nil {
		handleServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

func (h *Handler) release(writer http.ResponseWriter, request *http.Request) {
	var input ReleaseRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	result, err := h.service.Release(request.Context(), input)
	if err != nil {
		handleServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (h *Handler) getItem(writer http.ResponseWriter, request *http.Request) {
	result, err := h.service.GetItem(request.Context(), request.PathValue("sku"))
	if err != nil {
		handleServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func handleServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(writer, http.StatusNotFound, "inventory_not_found", err.Error())
	case errors.Is(err, ErrConflict):
		writeError(writer, http.StatusConflict, "reservation_conflict", err.Error())
	case errors.Is(err, ErrInsufficientStock):
		writeError(writer, http.StatusConflict, "insufficient_stock", err.Error())
	default:
		writeError(writer, http.StatusInternalServerError, "internal_error", "inventory operation failed")
	}
}

func decodeJSON(request *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(destination)
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]string{"code": code, "message": message})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
