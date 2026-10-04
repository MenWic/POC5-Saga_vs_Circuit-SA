package order

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInvalidInput indica una entrada inválida
	ErrInvalidInput = errors.New("invalid input")
	// ErrNotFound indica que la orden no existe
	ErrNotFound = errors.New("order not found")
	// ErrCancellationConflict indica que el estado no permite cancelación local
	ErrCancellationConflict = errors.New("order cannot be cancelled in its current state")
)

// Order representa una compra y el estado de su Saga
type Order struct {
	ID            int64     `json:"order_id"`
	SKU           string    `json:"sku"`
	Quantity      int       `json:"quantity"`
	AmountCents   int64     `json:"amount_cents"`
	Status        string    `json:"status"`
	PaymentID     *int64    `json:"payment_id,omitempty"`
	ShippingID    *int64    `json:"shipping_id,omitempty"`
	FailureReason *string   `json:"failure_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CreateRequest contiene los datos permitidos para una compra
type CreateRequest struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

// Payment contiene la referencia devuelta por Payment
type Payment struct {
	ID int64 `json:"id"`
}

// Shipment contiene la referencia devuelta por Shipping
type Shipment struct {
	ID int64 `json:"id"`
}

// RemoteError conserva el código HTTP de una dependencia
type RemoteError struct {
	StatusCode int
	Code       string
	Message    string
	Service    string
}

// Error describe el fallo remoto
func (e *RemoteError) Error() string {
	return fmt.Sprintf("%s: %s", e.Service, e.Message)
}

// SagaError describe el resultado fallido y su compensación
type SagaError struct {
	StatusCode  int
	Code        string
	Message     string
	Compensated bool
}

// Error describe el fallo de la Saga
func (e *SagaError) Error() string {
	return e.Message
}
