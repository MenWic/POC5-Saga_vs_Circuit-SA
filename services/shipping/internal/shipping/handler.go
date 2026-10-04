package shipping

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// Handler atiende la API HTTP de Shipping
type Handler struct {
	service *Service
}

// NewHandler crea las rutas HTTP de Shipping
func NewHandler(service *Service, demoMode bool) http.Handler {
	handler := &Handler{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("POST /shipping/schedule", handler.schedule)
	mux.HandleFunc("GET /shipping/{id}", handler.get)
	mux.HandleFunc("DELETE /shipping/{id}", handler.cancel)
	if demoMode {
		mux.HandleFunc("GET /demo/failure", handler.getFailure)
		mux.HandleFunc("PUT /demo/failure", handler.setFailure)
	}
	return mux
}

func (h *Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) schedule(writer http.ResponseWriter, request *http.Request) {
	var input ScheduleRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	result, err := h.service.Schedule(request.Context(), input)
	if err != nil {
		handleServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, result)
}

func (h *Handler) get(writer http.ResponseWriter, request *http.Request) {
	id, err := parseID(request.PathValue("id"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_id", "shipping id must be a positive integer")
		return
	}
	result, err := h.service.Get(request.Context(), id)
	if err != nil {
		handleServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (h *Handler) cancel(writer http.ResponseWriter, request *http.Request) {
	id, err := parseID(request.PathValue("id"))
	if err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_id", "shipping id must be a positive integer")
		return
	}
	result, err := h.service.Cancel(request.Context(), id)
	if err != nil {
		handleServiceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (h *Handler) getFailure(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, FailureRequest{Enabled: h.service.DemoFailure()})
}

func (h *Handler) setFailure(writer http.ResponseWriter, request *http.Request) {
	var input FailureRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	h.service.SetDemoFailure(input.Enabled)
	writeJSON(writer, http.StatusOK, input)
}

func handleServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeError(writer, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, ErrNotFound):
		writeError(writer, http.StatusNotFound, "shipment_not_found", err.Error())
	case errors.Is(err, ErrConflict):
		writeError(writer, http.StatusConflict, "shipment_conflict", err.Error())
	case errors.Is(err, ErrDemoFailure):
		writeError(writer, http.StatusServiceUnavailable, "shipping_unavailable", err.Error())
	default:
		writeError(writer, http.StatusInternalServerError, "internal_error", "shipping operation failed")
	}
}

func parseID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalidInput
	}
	return id, nil
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
