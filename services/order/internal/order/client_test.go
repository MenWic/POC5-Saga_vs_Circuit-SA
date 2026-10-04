package order

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestHTTPClientsUseExpectedContracts(t *testing.T) {
	requests := make([]string, 0, 6)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.Method+" "+request.URL.Path)
		status := http.StatusOK
		body := `{}`
		switch request.URL.Path {
		case "/payments":
			status = http.StatusCreated
			body = `{"id":11}`
		case "/shipping/schedule":
			status = http.StatusCreated
			body = `{"id":21}`
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}

	breakers := NewCircuitBreakers(time.Second, nil)
	payment := NewPaymentClient("http://payment.test", client, breakers.Payment)
	inventory := NewInventoryClient("http://inventory.test", client)
	shipping := NewShippingClient("http://shipping.test", client, breakers.Shipping)
	if _, err := payment.Charge(context.Background(), 1, 1000); err != nil {
		t.Fatalf("charge: %v", err)
	}
	if err := inventory.Reserve(context.Background(), 1, "SKU-001", 1); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := shipping.Schedule(context.Background(), 1); err != nil {
		t.Fatalf("schedule: %v", err)
	}
	if err := shipping.Cancel(context.Background(), 21); err != nil {
		t.Fatalf("cancel shipping: %v", err)
	}
	if err := inventory.Release(context.Background(), 1); err != nil {
		t.Fatalf("release inventory: %v", err)
	}
	if err := payment.Refund(context.Background(), 11); err != nil {
		t.Fatalf("refund: %v", err)
	}

	want := []string{
		"POST /payments",
		"POST /inventory/reserve",
		"POST /shipping/schedule",
		"DELETE /shipping/21",
		"POST /inventory/release",
		"POST /payments/11/refund",
	}
	assertEvents(t, requests, want)
}

func TestHTTPClientReturnsRemoteStatus(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusConflict,
			Status:     "409 Conflict",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":"insufficient_stock","message":"insufficient stock"}`)),
			Request:    request,
		}, nil
	})}

	err := NewInventoryClient("http://inventory.test", client).Reserve(context.Background(), 1, "SKU-001", 101)
	var remoteError *RemoteError
	if !errors.As(err, &remoteError) {
		t.Fatalf("error = %v, want RemoteError", err)
	}
	if remoteError.StatusCode != http.StatusConflict || remoteError.Code != "insufficient_stock" {
		t.Fatalf("remote error = %+v", remoteError)
	}
}
