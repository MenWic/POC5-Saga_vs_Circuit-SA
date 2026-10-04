package order

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"reflect"
	"testing"
	"time"
)

type fakeRepository struct {
	order            Order
	completeError    error
	setPaymentError  error
	setShippingError error
	markFailedError  error
}

func (f *fakeRepository) Create(_ context.Context, sku string, quantity int, amountCents int64) (Order, error) {
	f.order = Order{ID: 1, SKU: sku, Quantity: quantity, AmountCents: amountCents, Status: "PENDING", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return f.order, nil
}

func (f *fakeRepository) Get(_ context.Context, id int64) (Order, error) {
	if f.order.ID != id {
		return Order{}, ErrNotFound
	}
	return f.order, nil
}

func (f *fakeRepository) SetPaymentID(_ context.Context, id, paymentID int64) error {
	if f.setPaymentError != nil {
		return f.setPaymentError
	}
	f.order.PaymentID = &paymentID
	return nil
}

func (f *fakeRepository) SetShippingID(_ context.Context, id, shippingID int64) error {
	if f.setShippingError != nil {
		return f.setShippingError
	}
	f.order.ShippingID = &shippingID
	return nil
}

func (f *fakeRepository) Complete(_ context.Context, id int64) (Order, error) {
	if f.completeError != nil {
		return Order{}, f.completeError
	}
	f.order.Status = "COMPLETED"
	return f.order, nil
}

func (f *fakeRepository) MarkFailed(_ context.Context, id int64, status, reason string) (Order, error) {
	if f.markFailedError != nil {
		return Order{}, f.markFailedError
	}
	f.order.Status = status
	f.order.FailureReason = &reason
	return f.order, nil
}

func (f *fakeRepository) Cancel(_ context.Context, id int64, reason string) (Order, error) {
	if f.order.ID != id {
		return Order{}, ErrNotFound
	}
	switch f.order.Status {
	case "PENDING":
		f.order.Status = "CANCELLED"
		f.order.FailureReason = &reason
		return f.order, nil
	case "CANCELLED":
		return f.order, nil
	default:
		return Order{}, ErrCancellationConflict
	}
}

type fakePayment struct {
	events      *[]string
	chargeError error
	refundError error
}

func (f *fakePayment) Charge(context.Context, int64, int64) (Payment, error) {
	*f.events = append(*f.events, "charge")
	if f.chargeError != nil {
		return Payment{}, f.chargeError
	}
	return Payment{ID: 11}, nil
}

func (f *fakePayment) Refund(ctx context.Context, _ int64) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	*f.events = append(*f.events, "refund")
	return f.refundError
}

type fakeInventory struct {
	events       *[]string
	reserveError error
	releaseError error
}

func (f *fakeInventory) Reserve(context.Context, int64, string, int) error {
	*f.events = append(*f.events, "reserve")
	return f.reserveError
}

func (f *fakeInventory) Release(ctx context.Context, _ int64) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	*f.events = append(*f.events, "release")
	return f.releaseError
}

type fakeShipping struct {
	events        *[]string
	scheduleError error
	cancelError   error
}

func (f *fakeShipping) Schedule(context.Context, int64) (Shipment, error) {
	*f.events = append(*f.events, "schedule")
	if f.scheduleError != nil {
		return Shipment{}, f.scheduleError
	}
	return Shipment{ID: 21}, nil
}

func (f *fakeShipping) Cancel(ctx context.Context, _ int64) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	*f.events = append(*f.events, "cancel_shipping")
	return f.cancelError
}

func newTestService(repository *fakeRepository, payment *fakePayment, inventory *fakeInventory, shipping *fakeShipping) *Service {
	return NewService(repository, payment, inventory, shipping, time.Second, log.New(io.Discard, "", 0))
}

func TestSagaSuccessfulPurchase(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{}
	service := newTestService(repository, &fakePayment{events: &events}, &fakeInventory{events: &events}, &fakeShipping{events: &events})

	result, err := service.Create(context.Background(), CreateRequest{SKU: "SKU-001", Quantity: 2})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if result.Status != "COMPLETED" || result.AmountCents != 2000 {
		t.Fatalf("result = %+v", result)
	}
	if result.PaymentID == nil || *result.PaymentID != 11 || result.ShippingID == nil || *result.ShippingID != 21 {
		t.Fatalf("missing remote ids: %+v", result)
	}
	assertEvents(t, events, []string{"charge", "reserve", "schedule"})
}

func TestSagaPaymentFailureCancelsOrder(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{}
	paymentError := &RemoteError{StatusCode: http.StatusServiceUnavailable, Code: "payment_unavailable", Message: "demo failure", Service: "payment"}
	service := newTestService(repository, &fakePayment{events: &events, chargeError: paymentError}, &fakeInventory{events: &events}, &fakeShipping{events: &events})

	result, err := service.Create(context.Background(), CreateRequest{SKU: "SKU-001", Quantity: 1})
	assertSagaError(t, err, http.StatusServiceUnavailable, true)
	if result.Status != "CANCELLED" {
		t.Fatalf("status = %q, want CANCELLED", result.Status)
	}
	assertEvents(t, events, []string{"charge"})
}

func TestSagaInsufficientStockRefundsPayment(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{}
	inventoryError := &RemoteError{StatusCode: http.StatusConflict, Code: "insufficient_stock", Message: "insufficient stock", Service: "inventory"}
	service := newTestService(repository, &fakePayment{events: &events}, &fakeInventory{events: &events, reserveError: inventoryError}, &fakeShipping{events: &events})

	result, err := service.Create(context.Background(), CreateRequest{SKU: "SKU-001", Quantity: 101})
	assertSagaError(t, err, http.StatusConflict, true)
	if result.Status != "CANCELLED" {
		t.Fatalf("status = %q, want CANCELLED", result.Status)
	}
	assertEvents(t, events, []string{"charge", "reserve", "refund"})
}

func TestSagaShippingFailureReleasesThenRefunds(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{}
	shippingError := &RemoteError{StatusCode: http.StatusServiceUnavailable, Code: "shipping_unavailable", Message: "demo failure", Service: "shipping"}
	service := newTestService(repository, &fakePayment{events: &events}, &fakeInventory{events: &events}, &fakeShipping{events: &events, scheduleError: shippingError})

	result, err := service.Create(context.Background(), CreateRequest{SKU: "SKU-001", Quantity: 1})
	assertSagaError(t, err, http.StatusServiceUnavailable, true)
	if result.Status != "CANCELLED" {
		t.Fatalf("status = %q, want CANCELLED", result.Status)
	}
	assertEvents(t, events, []string{"charge", "reserve", "schedule", "release", "refund"})
}

func TestSagaLocalFailureAfterShippingCancelsAllInReverse(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{completeError: errors.New("database write failed")}
	service := newTestService(repository, &fakePayment{events: &events}, &fakeInventory{events: &events}, &fakeShipping{events: &events})

	result, err := service.Create(context.Background(), CreateRequest{SKU: "SKU-001", Quantity: 1})
	assertSagaError(t, err, http.StatusInternalServerError, true)
	if result.Status != "CANCELLED" {
		t.Fatalf("status = %q, want CANCELLED", result.Status)
	}
	assertEvents(t, events, []string{"charge", "reserve", "schedule", "cancel_shipping", "release", "refund"})
}

func TestSagaCompensationFailureIsPersistedAndOtherCompensationsContinue(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{}
	shippingError := &RemoteError{StatusCode: http.StatusServiceUnavailable, Code: "shipping_unavailable", Message: "demo failure", Service: "shipping"}
	service := newTestService(repository, &fakePayment{events: &events}, &fakeInventory{events: &events, releaseError: errors.New("inventory unavailable")}, &fakeShipping{events: &events, scheduleError: shippingError})

	result, err := service.Create(context.Background(), CreateRequest{SKU: "SKU-001", Quantity: 1})
	assertSagaError(t, err, http.StatusInternalServerError, false)
	if result.Status != "COMPENSATION_FAILED" {
		t.Fatalf("status = %q, want COMPENSATION_FAILED", result.Status)
	}
	assertEvents(t, events, []string{"charge", "reserve", "schedule", "release", "refund"})
}

func TestSagaUsesIndependentContextForCompensation(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{}
	requestContext, cancel := context.WithCancel(context.Background())
	cancel()
	payment := &fakePayment{events: &events}
	inventoryError := &RemoteError{StatusCode: http.StatusConflict, Code: "insufficient_stock", Message: "insufficient stock", Service: "inventory"}
	service := newTestService(repository, payment, &fakeInventory{events: &events, reserveError: inventoryError}, &fakeShipping{events: &events})

	result, err := service.Create(requestContext, CreateRequest{SKU: "SKU-001", Quantity: 1})
	assertSagaError(t, err, http.StatusConflict, true)
	if result.Status != "CANCELLED" {
		t.Fatalf("status = %q, want CANCELLED", result.Status)
	}
	assertEvents(t, events, []string{"charge", "reserve", "refund"})
}

func assertSagaError(t *testing.T, err error, status int, compensated bool) {
	t.Helper()
	var sagaError *SagaError
	if !errors.As(err, &sagaError) {
		t.Fatalf("error = %v, want SagaError", err)
	}
	if sagaError.StatusCode != status || sagaError.Compensated != compensated {
		t.Fatalf("saga error = %+v, want status=%d compensated=%t", sagaError, status, compensated)
	}
}

func assertEvents(t *testing.T, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}
