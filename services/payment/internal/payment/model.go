package payment

import (
	"errors"
	"time"
)

var (
	// ErrInvalidInput indica una entrada inválida
	ErrInvalidInput = errors.New("invalid input")
	// ErrNotFound indica que el pago no existe
	ErrNotFound = errors.New("payment not found")
	// ErrConflict indica que la orden ya tiene un pago
	ErrConflict = errors.New("payment already exists for order")
	// ErrDemoFailure indica una falla controlada antes del cobro
	ErrDemoFailure = errors.New("payment demo failure is enabled")
)

// Payment representa un cobro persistido
type Payment struct {
	ID          int64      `json:"id"`
	OrderID     int64      `json:"order_id"`
	AmountCents int64      `json:"amount_cents"`
	Status      string     `json:"status"`
	ChargedAt   time.Time  `json:"charged_at"`
	RefundedAt  *time.Time `json:"refunded_at,omitempty"`
}

// CreateRequest contiene los datos requeridos para cobrar
type CreateRequest struct {
	OrderID     int64 `json:"order_id"`
	AmountCents int64 `json:"amount_cents"`
}

// FailureRequest controla la falla de demostración
type FailureRequest struct {
	Enabled bool `json:"enabled"`
}
