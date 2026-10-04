package payment

import (
	"context"
	"sync"
)

// Service aplica las reglas locales de Payment
type Service struct {
	repository  Repository
	demoMode    bool
	failureLock sync.RWMutex
	failure     bool
}

// NewService crea la lógica de cobro y reembolso
func NewService(repository Repository, demoMode bool) *Service {
	return &Service{repository: repository, demoMode: demoMode}
}

// Create valida y registra un cobro
func (s *Service) Create(ctx context.Context, request CreateRequest) (Payment, error) {
	if request.OrderID <= 0 || request.AmountCents <= 0 {
		return Payment{}, ErrInvalidInput
	}
	if s.demoFailureEnabled() {
		return Payment{}, ErrDemoFailure
	}
	return s.repository.Create(ctx, request.OrderID, request.AmountCents)
}

// Get consulta un pago
func (s *Service) Get(ctx context.Context, id int64) (Payment, error) {
	if id <= 0 {
		return Payment{}, ErrInvalidInput
	}
	return s.repository.Get(ctx, id)
}

// Refund ejecuta la compensación del cobro
func (s *Service) Refund(ctx context.Context, id int64) (Payment, error) {
	if id <= 0 {
		return Payment{}, ErrInvalidInput
	}
	return s.repository.Refund(ctx, id)
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
