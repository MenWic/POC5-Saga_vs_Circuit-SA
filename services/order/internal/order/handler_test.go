package order

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCircuitBreakerDiagnosticEndpoint(t *testing.T) {
	breakers := NewCircuitBreakers(time.Second, nil)
	handler := NewHandler(nil, breakers)

	response := performRequest(handler, http.MethodGet, "/demo/circuit-breakers", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var snapshot CircuitBreakersSnapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode diagnostic response: %v", err)
	}
	if snapshot.Payment.State != "closed" || snapshot.Shipping.State != "closed" {
		t.Fatalf("states payment=%s shipping=%s, want closed", snapshot.Payment.State, snapshot.Shipping.State)
	}
	if snapshot.Payment.Executions != 0 || snapshot.Shipping.Executions != 0 {
		t.Fatalf("unexpected initial metrics: %+v", snapshot)
	}
}

func TestOrderEndpointMapsOpenPaymentCircuitTo503(t *testing.T) {
	breakers := NewCircuitBreakers(time.Second, nil)
	breakers.Payment.policy.Open()
	transport := &scriptedTransport{defaultStatus: http.StatusOK}
	events := []string{}
	repository := &fakeRepository{}
	service := NewService(
		repository,
		NewPaymentClient("http://payment.test", &http.Client{Transport: transport}, breakers.Payment),
		&fakeInventory{events: &events},
		&fakeShipping{events: &events},
		time.Second,
		nil,
	)
	handler := NewHandler(service, breakers)

	response := performRequest(handler, http.MethodPost, "/orders", `{"sku":"SKU-001","quantity":1}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"code":"payment_circuit_open"`) ||
		!strings.Contains(response.Body.String(), `"status":"CANCELLED"`) ||
		!strings.Contains(response.Body.String(), `"compensated":true`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
	if transport.callCount() != 0 {
		t.Fatalf("payment HTTP calls = %d, want 0", transport.callCount())
	}
}

func TestOrderEndpoints(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{}
	service := newTestService(repository, &fakePayment{events: &events}, &fakeInventory{events: &events}, &fakeShipping{events: &events})
	handler := NewHandler(service, NewCircuitBreakers(time.Second, nil))

	create := performRequest(handler, http.MethodPost, "/orders", `{"sku":"SKU-001","quantity":2}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", create.Code, create.Body.String())
	}
	get := performRequest(handler, http.MethodGet, "/orders/1", "")
	if get.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200", get.Code)
	}
	cancel := performRequest(handler, http.MethodDelete, "/orders/1", "")
	if cancel.Code != http.StatusConflict {
		t.Fatalf("cancel completed order status = %d, want 409", cancel.Code)
	}
	if !strings.Contains(cancel.Body.String(), `"code":"order_cancellation_conflict"`) {
		t.Fatalf("unexpected conflict response: %s", cancel.Body.String())
	}
	if repository.order.Status != "COMPLETED" {
		t.Fatalf("order status = %q, want COMPLETED", repository.order.Status)
	}
	assertEvents(t, events, []string{"charge", "reserve", "schedule"})
	health := performRequest(handler, http.MethodGet, "/health", "")
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}
}

func TestDeleteOrderStateTransitions(t *testing.T) {
	tests := []struct {
		name       string
		order      Order
		target     string
		wantStatus int
		wantState  string
		repeat     bool
	}{
		{
			name:       "pending order is cancelled",
			order:      Order{ID: 10, Status: "PENDING"},
			target:     "/orders/10",
			wantStatus: http.StatusOK,
			wantState:  "CANCELLED",
			repeat:     true,
		},
		{
			name:       "cancelled order is idempotent",
			order:      Order{ID: 11, Status: "CANCELLED"},
			target:     "/orders/11",
			wantStatus: http.StatusOK,
			wantState:  "CANCELLED",
			repeat:     true,
		},
		{
			name:       "completed order is rejected",
			order:      Order{ID: 12, Status: "COMPLETED"},
			target:     "/orders/12",
			wantStatus: http.StatusConflict,
			wantState:  "COMPLETED",
		},
		{
			name:       "compensation failure is preserved",
			order:      Order{ID: 13, Status: "COMPENSATION_FAILED"},
			target:     "/orders/13",
			wantStatus: http.StatusConflict,
			wantState:  "COMPENSATION_FAILED",
		},
		{
			name:       "missing order returns not found",
			order:      Order{},
			target:     "/orders/99",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "invalid id returns bad request",
			order:      Order{},
			target:     "/orders/not-a-number",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events := []string{}
			repository := &fakeRepository{order: test.order}
			service := newTestService(repository, &fakePayment{events: &events}, &fakeInventory{events: &events}, &fakeShipping{events: &events})
			handler := NewHandler(service, NewCircuitBreakers(time.Second, nil))

			response := performRequest(handler, http.MethodDelete, test.target, "")
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantState != "" && repository.order.Status != test.wantState {
				t.Fatalf("persisted status = %q, want %q", repository.order.Status, test.wantState)
			}
			if len(events) != 0 {
				t.Fatalf("local cancellation called remote services: %v", events)
			}
			if test.repeat {
				second := performRequest(handler, http.MethodDelete, test.target, "")
				if second.Code != http.StatusOK || repository.order.Status != "CANCELLED" {
					t.Fatalf("repeated cancel status=%d persisted=%q", second.Code, repository.order.Status)
				}
			}
		})
	}
}

func TestOrderHandlerReportsCompensatedFailure(t *testing.T) {
	events := []string{}
	repository := &fakeRepository{}
	paymentError := &RemoteError{StatusCode: http.StatusServiceUnavailable, Code: "payment_unavailable", Message: "demo failure", Service: "payment"}
	service := newTestService(repository, &fakePayment{events: &events, chargeError: paymentError}, &fakeInventory{events: &events}, &fakeShipping{events: &events})
	handler := NewHandler(service, NewCircuitBreakers(time.Second, nil))

	response := performRequest(handler, http.MethodPost, "/orders", `{"sku":"SKU-001","quantity":1}`)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"status":"CANCELLED"`) || !strings.Contains(response.Body.String(), `"compensated":true`) {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func performRequest(handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
