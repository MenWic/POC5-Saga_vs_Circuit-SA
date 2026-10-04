package shipping

import (
	"context"
	"sync"
)

// Service aplica las reglas locales de Shipping
type Service struct {
	repository  Repository
	demoMode    bool
	failureLock sync.RWMutex
	failure     bool
}

// NewService crea la lógica de programación y cancelación de envíos
func NewService(repository Repository, demoMode bool) *Service {
	return &Service{repository: repository, demoMode: demoMode}
}

// Schedule valida y programa un envío
func (s *Service) Schedule(ctx context.Context, request ScheduleRequest) (Shipment, error) {
	if request.OrderID <= 0 {
		return Shipment{}, ErrInvalidInput
	}
	if s.demoFailureEnabled() {
		return Shipment{}, ErrDemoFailure
	}
	return s.repository.Schedule(ctx, request.OrderID)
}

// Get consulta un envío
func (s *Service) Get(ctx context.Context, id int64) (Shipment, error) {
	if id <= 0 {
		return Shipment{}, ErrInvalidInput
	}
	return s.repository.Get(ctx, id)
}

// Cancel ejecuta la compensación del envío
func (s *Service) Cancel(ctx context.Context, id int64) (Shipment, error) {
	if id <= 0 {
		return Shipment{}, ErrInvalidInput
	}
	return s.repository.Cancel(ctx, id)
}

// SetDemoFailure activa o desactiva la falla controlada
func (s *Service) SetDemoFailure(enabled bool) {
	s.failureLock.Lock()
	defer s.failureLock.Unlock()
	s.failure = enabled
}

// DemoFailure informa el estado de la falla controlada
func (s *Service) DemoFailure() bool {
	s.failureLock.RLock()
	defer s.failureLock.RUnlock()
	return s.failure
}

func (s *Service) demoFailureEnabled() bool {
	return s.demoMode && s.DemoFailure()
}
