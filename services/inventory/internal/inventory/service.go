package inventory

import (
	"context"
	"strings"
)

// Service aplica las reglas locales de Inventory
type Service struct {
	repository Repository
}

// NewService crea la lógica de reservas de inventario
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// Reserve valida y solicita una reserva atómica
func (s *Service) Reserve(ctx context.Context, request ReserveRequest) (Reservation, error) {
	request.SKU = strings.TrimSpace(request.SKU)
	if request.OrderID <= 0 || request.SKU == "" || request.Quantity <= 0 {
		return Reservation{}, ErrInvalidInput
	}
	return s.repository.Reserve(ctx, request.OrderID, request.SKU, request.Quantity)
}

// Release ejecuta la compensación de inventario
func (s *Service) Release(ctx context.Context, request ReleaseRequest) (Reservation, error) {
	if request.OrderID <= 0 {
		return Reservation{}, ErrInvalidInput
	}
	return s.repository.Release(ctx, request.OrderID)
}

// GetItem consulta el stock disponible
func (s *Service) GetItem(ctx context.Context, sku string) (Item, error) {
	sku = strings.TrimSpace(sku)
	if sku == "" {
		return Item{}, ErrInvalidInput
	}
	return s.repository.GetItem(ctx, sku)
}
