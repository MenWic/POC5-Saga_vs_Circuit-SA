package inventory

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeRepository struct {
	item         Item
	reservations map[int64]Reservation
}

func newFakeRepository(stock int) *fakeRepository {
	return &fakeRepository{
		item:         Item{SKU: "SKU-001", AvailableQuantity: stock},
		reservations: make(map[int64]Reservation),
	}
}

func (f *fakeRepository) Reserve(_ context.Context, orderID int64, sku string, quantity int) (Reservation, error) {
	if _, exists := f.reservations[orderID]; exists {
		return Reservation{}, ErrConflict
	}
	if sku != f.item.SKU {
		return Reservation{}, ErrNotFound
	}
	if f.item.AvailableQuantity < quantity {
		return Reservation{}, ErrInsufficientStock
	}
	f.item.AvailableQuantity -= quantity
	reservation := Reservation{OrderID: orderID, SKU: sku, Quantity: quantity, Status: "RESERVED", CreatedAt: time.Now()}
	f.reservations[orderID] = reservation
	return reservation, nil
}

func (f *fakeRepository) Release(_ context.Context, orderID int64) (Reservation, error) {
	reservation, exists := f.reservations[orderID]
	if !exists {
		return Reservation{}, ErrNotFound
	}
	if reservation.Status == "RESERVED" {
		f.item.AvailableQuantity += reservation.Quantity
		now := time.Now()
		reservation.Status = "RELEASED"
		reservation.ReleasedAt = &now
		f.reservations[orderID] = reservation
	}
	return reservation, nil
}

func (f *fakeRepository) GetItem(_ context.Context, sku string) (Item, error) {
	if sku != f.item.SKU {
		return Item{}, ErrNotFound
	}
	return f.item, nil
}

func TestInventoryEndpointsReserveAndIdempotentRelease(t *testing.T) {
	repository := newFakeRepository(10)
	handler := NewHandler(NewService(repository))
	health := performRequest(handler, http.MethodGet, "/health", "")
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}

	reserve := performRequest(handler, http.MethodPost, "/inventory/reserve", `{"order_id":1,"sku":"SKU-001","quantity":3}`)
	if reserve.Code != http.StatusCreated {
		t.Fatalf("reserve status = %d, want 201: %s", reserve.Code, reserve.Body.String())
	}
	if repository.item.AvailableQuantity != 7 {
		t.Fatalf("stock = %d, want 7", repository.item.AvailableQuantity)
	}

	firstRelease := performRequest(handler, http.MethodPost, "/inventory/release", `{"order_id":1}`)
	secondRelease := performRequest(handler, http.MethodPost, "/inventory/release", `{"order_id":1}`)
	if firstRelease.Code != http.StatusOK || secondRelease.Code != http.StatusOK {
		t.Fatalf("release statuses = %d and %d, want 200", firstRelease.Code, secondRelease.Code)
	}
	if repository.item.AvailableQuantity != 10 {
		t.Fatalf("stock after repeated release = %d, want 10", repository.item.AvailableQuantity)
	}

	get := performRequest(handler, http.MethodGet, "/inventory/SKU-001", "")
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", get.Code)
	}
}

func TestInsufficientStockDoesNotChangeInventory(t *testing.T) {
	repository := newFakeRepository(2)
	handler := NewHandler(NewService(repository))

	response := performRequest(handler, http.MethodPost, "/inventory/reserve", `{"order_id":2,"sku":"SKU-001","quantity":3}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", response.Code)
	}
	if repository.item.AvailableQuantity != 2 || len(repository.reservations) != 0 {
		t.Fatalf("insufficient reserve changed data: stock=%d reservations=%d", repository.item.AvailableQuantity, len(repository.reservations))
	}
}

func performRequest(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
