package inventory

import (
	"errors"
	"time"
)

var (
	// ErrInvalidInput indica una entrada inválida
	ErrInvalidInput = errors.New("invalid input")
	// ErrNotFound indica que el recurso de inventario no existe
	ErrNotFound = errors.New("inventory resource not found")
	// ErrConflict indica que la orden ya tiene una reserva
	ErrConflict = errors.New("reservation already exists for order")
	// ErrInsufficientStock indica que no hay unidades suficientes
	ErrInsufficientStock = errors.New("insufficient stock")
)

// Item representa las existencias de un SKU
type Item struct {
	SKU               string `json:"sku"`
	AvailableQuantity int    `json:"available_quantity"`
}

// Reservation representa el stock asignado a una orden
type Reservation struct {
	OrderID    int64      `json:"order_id"`
	SKU        string     `json:"sku"`
	Quantity   int        `json:"quantity"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ReleasedAt *time.Time `json:"released_at,omitempty"`
}

// ReserveRequest contiene los datos de una reserva
type ReserveRequest struct {
	OrderID  int64  `json:"order_id"`
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

// ReleaseRequest identifica la reserva que debe liberarse
type ReleaseRequest struct {
	OrderID int64 `json:"order_id"`
}
