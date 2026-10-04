package shipping

import (
	"errors"
	"time"
)

var (
	// ErrInvalidInput indica una entrada inválida
	ErrInvalidInput = errors.New("invalid input")
	// ErrNotFound indica que el envío no existe
	ErrNotFound = errors.New("shipment not found")
	// ErrConflict indica que la orden ya tiene un envío
	ErrConflict = errors.New("shipment already exists for order")
	// ErrDemoFailure indica una falla controlada antes de programar
	ErrDemoFailure = errors.New("shipping demo failure is enabled")
)

// Shipment representa un envío persistido
type Shipment struct {
	ID          int64      `json:"id"`
	OrderID     int64      `json:"order_id"`
	Status      string     `json:"status"`
	ScheduledAt time.Time  `json:"scheduled_at"`
	CancelledAt *time.Time `json:"cancelled_at,omitempty"`
}

// ScheduleRequest identifica la orden que requiere envío
type ScheduleRequest struct {
	OrderID int64 `json:"order_id"`
}

// FailureRequest controla la falla de demostración
type FailureRequest struct {
	Enabled bool `json:"enabled"`
}
