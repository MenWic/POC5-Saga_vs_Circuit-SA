package order

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"
)

const (
	demoSKU        = "SKU-001"
	unitPriceCents = int64(1000)
)

// Service coordina la Saga de compra
type Service struct {
	repository          Repository
	payment             PaymentGateway
	inventory           InventoryGateway
	shipping            ShippingGateway
	compensationTimeout time.Duration
	logger              *log.Logger
}

type sagaProgress struct {
	paymentID         int64
	inventoryReserved bool
	shippingID        int64
}

// NewService crea el orquestador secuencial de la Saga
func NewService(
	repository Repository,
	payment PaymentGateway,
	inventory InventoryGateway,
	shipping ShippingGateway,
	compensationTimeout time.Duration,
	logger *log.Logger,
) *Service {
	return &Service{
		repository:          repository,
		payment:             payment,
		inventory:           inventory,
		shipping:            shipping,
		compensationTimeout: compensationTimeout,
		logger:              logger,
	}
}

// Create ejecuta el flujo completo de compra
func (s *Service) Create(ctx context.Context, request CreateRequest) (Order, error) {
	request.SKU = strings.TrimSpace(request.SKU)
	if request.SKU != demoSKU || request.Quantity <= 0 || int64(request.Quantity) > math.MaxInt64/unitPriceCents {
		return Order{}, ErrInvalidInput
	}

	amountCents := int64(request.Quantity) * unitPriceCents
	currentOrder, err := s.repository.Create(ctx, request.SKU, request.Quantity, amountCents)
	if err != nil {
		return Order{}, fmt.Errorf("create pending order: %w", err)
	}
	s.log(currentOrder.ID, "order", "PENDING")
	progress := sagaProgress{}

	payment, err := s.payment.Charge(ctx, currentOrder.ID, currentOrder.AmountCents)
	if err != nil {
		return s.failSaga(&currentOrder, err, progress)
	}
	progress.paymentID = payment.ID
	currentOrder.PaymentID = &payment.ID
	s.log(currentOrder.ID, "payment", "CHARGED")
	if err := s.repository.SetPaymentID(ctx, currentOrder.ID, payment.ID); err != nil {
		return s.failSaga(&currentOrder, fmt.Errorf("persist payment reference: %w", err), progress)
	}

	if err := s.inventory.Reserve(ctx, currentOrder.ID, currentOrder.SKU, currentOrder.Quantity); err != nil {
		return s.failSaga(&currentOrder, err, progress)
	}
	progress.inventoryReserved = true
	s.log(currentOrder.ID, "inventory", "RESERVED")

	shipment, err := s.shipping.Schedule(ctx, currentOrder.ID)
	if err != nil {
		return s.failSaga(&currentOrder, err, progress)
	}
	progress.shippingID = shipment.ID
	currentOrder.ShippingID = &shipment.ID
	s.log(currentOrder.ID, "shipping", "SCHEDULED")
	if err := s.repository.SetShippingID(ctx, currentOrder.ID, shipment.ID); err != nil {
		return s.failSaga(&currentOrder, fmt.Errorf("persist shipping reference: %w", err), progress)
	}

	completed, err := s.repository.Complete(ctx, currentOrder.ID)
	if err != nil {
		return s.failSaga(&currentOrder, fmt.Errorf("complete order: %w", err), progress)
	}
	s.log(currentOrder.ID, "order", "COMPLETED")
	return completed, nil
}

// Get consulta una orden
func (s *Service) Get(ctx context.Context, id int64) (Order, error) {
	if id <= 0 {
		return Order{}, ErrInvalidInput
	}
	return s.repository.Get(ctx, id)
}

// Cancel ejecuta la cancelación lógica local
func (s *Service) Cancel(ctx context.Context, id int64) (Order, error) {
	if id <= 0 {
		return Order{}, ErrInvalidInput
	}
	return s.repository.Cancel(ctx, id, "cancelled by request")
}

func (s *Service) failSaga(currentOrder *Order, cause error, progress sagaProgress) (Order, error) {
	// La limpieza usa un contexto nuevo porque la petición original puede estar cancelada
	compensationContext, cancel := context.WithTimeout(context.Background(), s.compensationTimeout)
	defer cancel()

	var compensationErrors []string
	if progress.shippingID != 0 {
		if err := s.shipping.Cancel(compensationContext, progress.shippingID); err != nil {
			compensationErrors = append(compensationErrors, "cancel shipping: "+err.Error())
			s.log(currentOrder.ID, "compensate_shipping", "FAILED")
		} else {
			s.log(currentOrder.ID, "compensate_shipping", "CANCELLED")
		}
	}
	if progress.inventoryReserved {
		if err := s.inventory.Release(compensationContext, currentOrder.ID); err != nil {
			compensationErrors = append(compensationErrors, "release inventory: "+err.Error())
			s.log(currentOrder.ID, "compensate_inventory", "FAILED")
		} else {
			s.log(currentOrder.ID, "compensate_inventory", "RELEASED")
		}
	}
	if progress.paymentID != 0 {
		if err := s.payment.Refund(compensationContext, progress.paymentID); err != nil {
			compensationErrors = append(compensationErrors, "refund payment: "+err.Error())
			s.log(currentOrder.ID, "compensate_payment", "FAILED")
		} else {
			s.log(currentOrder.ID, "compensate_payment", "REFUNDED")
		}
	}

	status := "CANCELLED"
	reason := cause.Error()
	if len(compensationErrors) > 0 {
		status = "COMPENSATION_FAILED"
		reason += "; compensation errors: " + strings.Join(compensationErrors, "; ")
	}
	updatedOrder, markError := s.repository.MarkFailed(compensationContext, currentOrder.ID, status, reason)
	if markError != nil {
		status = "COMPENSATION_FAILED"
		compensationErrors = append(compensationErrors, "persist failed state: "+markError.Error())
		currentOrder.Status = status
		currentOrder.FailureReason = &reason
		updatedOrder = *currentOrder
	}
	s.log(currentOrder.ID, "order", status)

	if len(compensationErrors) > 0 {
		return updatedOrder, &SagaError{
			StatusCode:  http.StatusInternalServerError,
			Code:        "compensation_failed",
			Message:     reason,
			Compensated: false,
		}
	}

	statusCode, code := classifyCause(cause)
	return updatedOrder, &SagaError{
		StatusCode:  statusCode,
		Code:        code,
		Message:     cause.Error(),
		Compensated: true,
	}
}

func classifyCause(err error) (int, string) {
	var remoteError *RemoteError
	if errors.As(err, &remoteError) {
		status := remoteError.StatusCode
		if status < 400 || status > 599 {
			status = http.StatusServiceUnavailable
		}
		if status >= 500 {
			status = http.StatusServiceUnavailable
		}
		return status, remoteError.Code
	}
	return http.StatusInternalServerError, "order_processing_failed"
}

func (s *Service) log(orderID int64, step, status string) {
	if s.logger != nil {
		s.logger.Printf("order_id=%d step=%s status=%s", orderID, step, status)
	}
}
