package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeRepository struct {
	payment     Payment
	createCalls int
	refundCalls int
}

func (f *fakeRepository) Create(_ context.Context, orderID, amountCents int64) (Payment, error) {
	f.createCalls++
	if f.payment.ID != 0 {
		return Payment{}, ErrConflict
	}
	f.payment = Payment{
		ID:          1,
		OrderID:     orderID,
		AmountCents: amountCents,
		Status:      "CHARGED",
		ChargedAt:   time.Now(),
	}
	return f.payment, nil
}

func (f *fakeRepository) Get(_ context.Context, id int64) (Payment, error) {
	if f.payment.ID != id {
		return Payment{}, ErrNotFound
	}
	return f.payment, nil
}

func (f *fakeRepository) Refund(_ context.Context, id int64) (Payment, error) {
	f.refundCalls++
	if f.payment.ID != id {
		return Payment{}, ErrNotFound
	}
	if f.payment.Status == "CHARGED" {
		now := time.Now()
		f.payment.Status = "REFUNDED"
		f.payment.RefundedAt = &now
	}
	return f.payment, nil
}

func TestPaymentEndpointsAndIdempotentRefund(t *testing.T) {
	repository := &fakeRepository{}
	handler := NewHandler(NewService(repository, true), true)
	health := performRequest(handler, http.MethodGet, "/health", "")
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}

	create := performRequest(handler, http.MethodPost, "/payments", `{"order_id":7,"amount_cents":2000}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d: %s", create.Code, http.StatusCreated, create.Body.String())
	}

	get := performRequest(handler, http.MethodGet, "/payments/1", "")
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d", get.Code, http.StatusOK)
	}

	firstRefund := performRequest(handler, http.MethodPost, "/payments/1/refund", "")
	secondRefund := performRequest(handler, http.MethodPost, "/payments/1/refund", "")
	if firstRefund.Code != http.StatusOK || secondRefund.Code != http.StatusOK {
		t.Fatalf("refund statuses = %d and %d, want 200", firstRefund.Code, secondRefund.Code)
	}
	var result Payment
	if err := json.Unmarshal(secondRefund.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode refund response: %v", err)
	}
	if result.Status != "REFUNDED" {
		t.Fatalf("refund status = %q, want REFUNDED", result.Status)
	}
}

func TestDemoFailureStopsChargeBeforeRepository(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, true)
	handler := NewHandler(service, true)

	setFailure := performRequest(handler, http.MethodPut, "/demo/failure", `{"enabled":true}`)
	if setFailure.Code != http.StatusOK {
		t.Fatalf("set failure status = %d, want 200", setFailure.Code)
	}
	create := performRequest(handler, http.MethodPost, "/payments", `{"order_id":8,"amount_cents":1000}`)
	if create.Code != http.StatusServiceUnavailable {
		t.Fatalf("create status = %d, want 503", create.Code)
	}
	if repository.createCalls != 0 {
		t.Fatalf("repository create calls = %d, want 0", repository.createCalls)
	}
}

func TestDemoRoutesAreAbsentWhenDisabled(t *testing.T) {
	handler := NewHandler(NewService(&fakeRepository{}, false), false)
	response := performRequest(handler, http.MethodGet, "/demo/failure", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func TestServiceErrors(t *testing.T) {
	repository := &errorRepository{err: errors.New("database unavailable")}
	handler := NewHandler(NewService(repository, false), false)
	response := performRequest(handler, http.MethodPost, "/payments", `{"order_id":1,"amount_cents":1000}`)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
}

type errorRepository struct {
	err error
}

func (e *errorRepository) Create(context.Context, int64, int64) (Payment, error) {
	return Payment{}, e.err
}

func (e *errorRepository) Get(context.Context, int64) (Payment, error) {
	return Payment{}, e.err
}

func (e *errorRepository) Refund(context.Context, int64) (Payment, error) {
	return Payment{}, e.err
}

func performRequest(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
