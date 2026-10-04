package order

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
)

// Handler atiende la API HTTP de Order
type Handler struct {
	service  *Service
	breakers *CircuitBreakers
}

type errorResponse struct {
	Code        string `json:"code"`
	Message     string `json:"message"`
	OrderID     int64  `json:"order_id,omitempty"`
	Status      string `json:"status,omitempty"`
	Compensated bool   `json:"compensated"`
}

// NewHandler crea las rutas HTTP de Order
func NewHandler(service *Service, breakers *CircuitBreakers) http.Handler {
	handler := &Handler{service: service, breakers: breakers}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handler.health)
	mux.HandleFunc("GET /demo/circuit-breakers", handler.circuitBreakers)
	mux.HandleFunc("POST /orders", handler.create)
	mux.HandleFunc("GET /orders/{id}", handler.get)
	mux.HandleFunc("DELETE /orders/{id}", handler.cancel)
	return mux
}

func (h *Handler) circuitBreakers(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, h.breakers.Snapshot())
}

func (h *Handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) create(writer http.ResponseWriter, request *http.Request) {
	var input CreateRequest
	if err := decodeJSON(request, &input); err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Code: "invalid_request", Message: "invalid JSON body"})
		return
	}

	result, err := h.service.Create(request.Context(), input)
	if err == nil {
		writeJSON(writer, http.StatusCreated, result)
		return
	}
	if errors.Is(err, ErrInvalidInput) {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Code: "invalid_request", Message: "sku must be SKU-001 and quantity must be positive"})
		return
	}
	var sagaError *SagaError
	if errors.As(err, &sagaError) {
		writeJSON(writer, sagaError.StatusCode, errorResponse{
			Code:        sagaError.Code,
			Message:     sagaError.Message,
			OrderID:     result.ID,
			Status:      result.Status,
			Compensated: sagaError.Compensated,
		})
		return
	}
	writeJSON(writer, http.StatusInternalServerError, errorResponse{Code: "internal_error", Message: "order operation failed"})
}

func (h *Handler) get(writer http.ResponseWriter, request *http.Request) {
	id, err := parseID(request.PathValue("id"))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Code: "invalid_id", Message: "order id must be a positive integer"})
		return
	}
	result, err := h.service.Get(request.Context(), id)
	if err != nil {
		handleLookupError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func (h *Handler) cancel(writer http.ResponseWriter, request *http.Request) {
	id, err := parseID(request.PathValue("id"))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, errorResponse{Code: "invalid_id", Message: "order id must be a positive integer"})
		return
	}
	result, err := h.service.Cancel(request.Context(), id)
	if err != nil {
		handleLookupError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}

func handleLookupError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeJSON(writer, http.StatusBadRequest, errorResponse{Code: "invalid_request", Message: err.Error()})
	case errors.Is(err, ErrNotFound):
		writeJSON(writer, http.StatusNotFound, errorResponse{Code: "order_not_found", Message: err.Error()})
	case errors.Is(err, ErrCancellationConflict):
		writeJSON(writer, http.StatusConflict, errorResponse{Code: "order_cancellation_conflict", Message: err.Error()})
	default:
		writeJSON(writer, http.StatusInternalServerError, errorResponse{Code: "internal_error", Message: "order operation failed"})
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

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
