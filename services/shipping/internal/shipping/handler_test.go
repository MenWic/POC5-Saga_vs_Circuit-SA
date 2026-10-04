package shipping

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeRepository struct {
	shipment      Shipment
	scheduleCalls int
}

func (f *fakeRepository) Schedule(_ context.Context, orderID int64) (Shipment, error) {
	f.scheduleCalls++
	if f.shipment.ID != 0 {
		return Shipment{}, ErrConflict
	}
	f.shipment = Shipment{ID: 1, OrderID: orderID, Status: "SCHEDULED", ScheduledAt: time.Now()}
	return f.shipment, nil
}

func (f *fakeRepository) Get(_ context.Context, id int64) (Shipment, error) {
	if f.shipment.ID != id {
		return Shipment{}, ErrNotFound
	}
	return f.shipment, nil
}

func (f *fakeRepository) Cancel(_ context.Context, id int64) (Shipment, error) {
	if f.shipment.ID != id {
		return Shipment{}, ErrNotFound
	}
	if f.shipment.Status == "SCHEDULED" {
		now := time.Now()
		f.shipment.Status = "CANCELLED"
		f.shipment.CancelledAt = &now
	}
	return f.shipment, nil
}

func TestShippingEndpointsAndIdempotentCancellation(t *testing.T) {
	repository := &fakeRepository{}
	handler := NewHandler(NewService(repository, true), true)
	health := performRequest(handler, http.MethodGet, "/health", "")
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}

	schedule := performRequest(handler, http.MethodPost, "/shipping/schedule", `{"order_id":7}`)
	if schedule.Code != http.StatusCreated {
		t.Fatalf("schedule status = %d, want 201: %s", schedule.Code, schedule.Body.String())
	}
	get := performRequest(handler, http.MethodGet, "/shipping/1", "")
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", get.Code)
	}

	firstCancel := performRequest(handler, http.MethodDelete, "/shipping/1", "")
	secondCancel := performRequest(handler, http.MethodDelete, "/shipping/1", "")
	if firstCancel.Code != http.StatusOK || secondCancel.Code != http.StatusOK {
		t.Fatalf("cancel statuses = %d and %d, want 200", firstCancel.Code, secondCancel.Code)
	}
	var result Shipment
	if err := json.Unmarshal(secondCancel.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode cancellation: %v", err)
	}
	if result.Status != "CANCELLED" {
		t.Fatalf("status = %q, want CANCELLED", result.Status)
	}
}

func TestDemoFailureStopsScheduleBeforeRepository(t *testing.T) {
	repository := &fakeRepository{}
	service := NewService(repository, true)
	handler := NewHandler(service, true)

	setFailure := performRequest(handler, http.MethodPut, "/demo/failure", `{"enabled":true}`)
	if setFailure.Code != http.StatusOK {
		t.Fatalf("set failure status = %d, want 200", setFailure.Code)
	}
	schedule := performRequest(handler, http.MethodPost, "/shipping/schedule", `{"order_id":8}`)
	if schedule.Code != http.StatusServiceUnavailable {
		t.Fatalf("schedule status = %d, want 503", schedule.Code)
	}
	if repository.scheduleCalls != 0 {
		t.Fatalf("repository schedule calls = %d, want 0", repository.scheduleCalls)
	}
}

func TestDemoRoutesAreAbsentWhenDisabled(t *testing.T) {
	handler := NewHandler(NewService(&fakeRepository{}, false), false)
	response := performRequest(handler, http.MethodGet, "/demo/failure", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}

func performRequest(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
