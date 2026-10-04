package order

import (
	"errors"
	"log"
	"net"
	"time"

	"github.com/failsafe-go/failsafe-go/circuitbreaker"
)

const (
	circuitFailureThreshold = uint(5)
	circuitWindowSize       = uint(10)
	circuitSuccessThreshold = uint(3)
)

// Breaker protege una operación principal de una dependencia
type Breaker struct {
	policy circuitbreaker.CircuitBreaker[any]
}

// CircuitBreakers agrupa las políticas independientes de Payment y Shipping
type CircuitBreakers struct {
	Payment  *Breaker
	Shipping *Breaker
}

// CircuitBreakerSnapshot expone el estado y las métricas reales de una política
type CircuitBreakerSnapshot struct {
	State            string  `json:"state"`
	Executions       uint    `json:"executions"`
	Failures         uint    `json:"failures"`
	FailureRate      float64 `json:"failure_rate"`
	Successes        uint    `json:"successes"`
	SuccessRate      float64 `json:"success_rate"`
	RemainingDelayMS int64   `json:"remaining_delay_ms"`
}

// CircuitBreakersSnapshot contiene el diagnóstico de ambas dependencias
type CircuitBreakersSnapshot struct {
	Payment  CircuitBreakerSnapshot `json:"payment"`
	Shipping CircuitBreakerSnapshot `json:"shipping"`
}

// NewCircuitBreakers crea una instancia reutilizable por dependencia
func NewCircuitBreakers(openDelay time.Duration, logger *log.Logger) *CircuitBreakers {
	return &CircuitBreakers{
		Payment:  newBreaker("payment", openDelay, logger),
		Shipping: newBreaker("shipping", openDelay, logger),
	}
}

func newBreaker(name string, openDelay time.Duration, logger *log.Logger) *Breaker {
	policy := circuitbreaker.NewBuilder[any]().
		HandleIf(func(_ any, err error) bool {
			return isTechnicalFailure(err)
		}).
		WithFailureThresholdRatio(circuitFailureThreshold, circuitWindowSize).
		WithDelay(openDelay).
		WithSuccessThreshold(circuitSuccessThreshold).
		OnStateChanged(func(event circuitbreaker.StateChangedEvent) {
			if logger != nil {
				logger.Printf("circuit_breaker=%s old_state=%s new_state=%s", name, event.OldState, event.NewState)
			}
		}).
		Build()
	return &Breaker{policy: policy}
}

func isTechnicalFailure(err error) bool {
	if err == nil {
		return false
	}
	var remoteError *RemoteError
	if errors.As(err, &remoteError) {
		return remoteError.StatusCode >= 500 && remoteError.StatusCode <= 599
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

// Snapshot obtiene el diagnóstico actual de ambas políticas
func (c *CircuitBreakers) Snapshot() CircuitBreakersSnapshot {
	return CircuitBreakersSnapshot{
		Payment:  c.Payment.snapshot(),
		Shipping: c.Shipping.snapshot(),
	}
}

func (b *Breaker) snapshot() CircuitBreakerSnapshot {
	metrics := b.policy.Metrics()
	return CircuitBreakerSnapshot{
		State:            b.policy.State().String(),
		Executions:       metrics.Executions(),
		Failures:         metrics.Failures(),
		FailureRate:      metrics.FailureRate(),
		Successes:        metrics.Successes(),
		SuccessRate:      metrics.SuccessRate(),
		RemainingDelayMS: b.policy.RemainingDelay().Milliseconds(),
	}
}
