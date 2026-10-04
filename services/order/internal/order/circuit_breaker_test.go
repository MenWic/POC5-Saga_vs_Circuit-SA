package order

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type scriptedTransport struct {
	mu            sync.Mutex
	statuses      []int
	defaultStatus int
	calls         int
	paths         []string
}

func (transport *scriptedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	transport.calls++
	transport.paths = append(transport.paths, request.Method+" "+request.URL.Path)
	status := transport.defaultStatus
	if len(transport.statuses) > 0 {
		status = transport.statuses[0]
		transport.statuses = transport.statuses[1:]
	}
	body := `{"code":"demo_failure","message":"demo failure"}`
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		switch request.URL.Path {
		case "/payments":
			body = `{"id":11}`
		case "/shipping/schedule":
			body = `{"id":21}`
		default:
			body = `{}`
		}
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

func (transport *scriptedTransport) callCount() int {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.calls
}

func TestPaymentAndShippingCircuitBreakersOpenAndBlockCalls(t *testing.T) {
	tests := []struct {
		name        string
		service     string
		execute     func(context.Context, *http.Client, *CircuitBreakers) error
		breaker     func(*CircuitBreakers) *Breaker
		wantErrCode string
	}{
		{
			name:    "payment",
			service: "payment",
			execute: func(ctx context.Context, client *http.Client, breakers *CircuitBreakers) error {
				_, err := NewPaymentClient("http://payment.test", client, breakers.Payment).Charge(ctx, 1, 1000)
				return err
			},
			breaker:     func(breakers *CircuitBreakers) *Breaker { return breakers.Payment },
			wantErrCode: "payment_circuit_open",
		},
		{
			name:    "shipping",
			service: "shipping",
			execute: func(ctx context.Context, client *http.Client, breakers *CircuitBreakers) error {
				_, err := NewShippingClient("http://shipping.test", client, breakers.Shipping).Schedule(ctx, 1)
				return err
			},
			breaker:     func(breakers *CircuitBreakers) *Breaker { return breakers.Shipping },
			wantErrCode: "shipping_circuit_open",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statuses := []int{
				http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK,
				http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable,
				http.StatusServiceUnavailable, http.StatusServiceUnavailable,
			}
			transport := &scriptedTransport{statuses: statuses, defaultStatus: http.StatusOK}
			client := &http.Client{Transport: transport}
			breakers := NewCircuitBreakers(time.Second, nil)

			for index := 0; index < 10; index++ {
				err := test.execute(context.Background(), client, breakers)
				if index < 5 && err != nil {
					t.Fatalf("successful execution %d: %v", index+1, err)
				}
				if index >= 5 && err == nil {
					t.Fatalf("technical failure %d returned nil", index-4)
				}
			}

			if !test.breaker(breakers).policy.IsOpen() {
				t.Fatalf("%s circuit state = %s, want open", test.service, test.breaker(breakers).policy.State())
			}
			metrics := test.breaker(breakers).policy.Metrics()
			if metrics.Executions() != 10 || metrics.Failures() != 5 {
				t.Fatalf("metrics executions=%d failures=%d, want 10 and 5", metrics.Executions(), metrics.Failures())
			}

			err := test.execute(context.Background(), client, breakers)
			var remoteError *RemoteError
			if !errors.As(err, &remoteError) || remoteError.StatusCode != http.StatusServiceUnavailable || remoteError.Code != test.wantErrCode {
				t.Fatalf("open circuit error = %#v, want code %s", err, test.wantErrCode)
			}
			if transport.callCount() != 10 {
				t.Fatalf("HTTP calls = %d, want 10 after open rejection", transport.callCount())
			}
		})
	}
}

func TestClientErrors4xxDoNotOpenCircuit(t *testing.T) {
	transport := &scriptedTransport{defaultStatus: http.StatusConflict}
	client := &http.Client{Transport: transport}
	breakers := NewCircuitBreakers(time.Second, nil)
	payment := NewPaymentClient("http://payment.test", client, breakers.Payment)

	for index := 0; index < 12; index++ {
		_, err := payment.Charge(context.Background(), int64(index+1), 1000)
		var remoteError *RemoteError
		if !errors.As(err, &remoteError) || remoteError.StatusCode != http.StatusConflict {
			t.Fatalf("execution %d error = %#v, want 409 RemoteError", index+1, err)
		}
	}

	if !breakers.Payment.policy.IsClosed() {
		t.Fatalf("payment circuit state = %s, want closed", breakers.Payment.policy.State())
	}
	metrics := breakers.Payment.policy.Metrics()
	if metrics.Failures() != 0 || transport.callCount() != 12 {
		t.Fatalf("failures=%d calls=%d, want 0 and 12", metrics.Failures(), transport.callCount())
	}
}

func TestCircuitRecoversAfterThreeSuccessfulProbes(t *testing.T) {
	delay := 20 * time.Millisecond
	statuses := []int{
		http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK,
		http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable,
		http.StatusServiceUnavailable, http.StatusServiceUnavailable,
	}
	transport := &scriptedTransport{statuses: statuses, defaultStatus: http.StatusOK}
	breakers := NewCircuitBreakers(delay, nil)
	payment := NewPaymentClient("http://payment.test", &http.Client{Transport: transport}, breakers.Payment)
	for index := 0; index < 10; index++ {
		_, _ = payment.Charge(context.Background(), int64(index+1), 1000)
	}
	if !breakers.Payment.policy.IsOpen() {
		t.Fatalf("state = %s, want open", breakers.Payment.policy.State())
	}

	time.Sleep(delay + 20*time.Millisecond)
	for probe := 1; probe <= 3; probe++ {
		if _, err := payment.Charge(context.Background(), int64(100+probe), 1000); err != nil {
			t.Fatalf("probe %d: %v", probe, err)
		}
		if probe < 3 && !breakers.Payment.policy.IsHalfOpen() {
			t.Fatalf("state after probe %d = %s, want half-open", probe, breakers.Payment.policy.State())
		}
	}
	if !breakers.Payment.policy.IsClosed() {
		t.Fatalf("state after three probes = %s, want closed", breakers.Payment.policy.State())
	}
}

func TestFailureDuringHalfOpenReopensCircuit(t *testing.T) {
	delay := 20 * time.Millisecond
	statuses := []int{
		http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK,
		http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable,
		http.StatusServiceUnavailable, http.StatusServiceUnavailable,
		http.StatusServiceUnavailable,
	}
	transport := &scriptedTransport{statuses: statuses, defaultStatus: http.StatusOK}
	breakers := NewCircuitBreakers(delay, nil)
	payment := NewPaymentClient("http://payment.test", &http.Client{Transport: transport}, breakers.Payment)
	for index := 0; index < 10; index++ {
		_, _ = payment.Charge(context.Background(), int64(index+1), 1000)
	}

	time.Sleep(delay + 20*time.Millisecond)
	if _, err := payment.Charge(context.Background(), 100, 1000); err == nil {
		t.Fatal("half-open technical failure returned nil")
	}
	if !breakers.Payment.policy.IsOpen() {
		t.Fatalf("state after failed probe = %s, want open", breakers.Payment.policy.State())
	}
	if breakers.Payment.policy.RemainingDelay() <= 0 {
		t.Fatal("reopened circuit has no remaining delay")
	}
}

func TestHalfOpenAllowsAtMostThreeConcurrentPermits(t *testing.T) {
	breaker := NewCircuitBreakers(time.Second, nil).Payment
	breaker.policy.HalfOpen()
	for permit := 1; permit <= 3; permit++ {
		if !breaker.policy.TryAcquirePermit() {
			t.Fatalf("permit %d was rejected", permit)
		}
	}
	if breaker.policy.TryAcquirePermit() {
		t.Fatal("fourth simultaneous permit was accepted")
	}
	for probe := 1; probe <= 3; probe++ {
		breaker.policy.RecordSuccess()
	}
	if !breaker.policy.IsClosed() {
		t.Fatalf("state after three successful permits = %s, want closed", breaker.policy.State())
	}
}

func TestCircuitBreakersAreIndependent(t *testing.T) {
	tests := []struct {
		name      string
		open      func(*CircuitBreakers) *Breaker
		untouched func(*CircuitBreakers) *Breaker
	}{
		{
			name:      "payment does not change shipping",
			open:      func(breakers *CircuitBreakers) *Breaker { return breakers.Payment },
			untouched: func(breakers *CircuitBreakers) *Breaker { return breakers.Shipping },
		},
		{
			name:      "shipping does not change payment",
			open:      func(breakers *CircuitBreakers) *Breaker { return breakers.Shipping },
			untouched: func(breakers *CircuitBreakers) *Breaker { return breakers.Payment },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			breakers := NewCircuitBreakers(time.Second, nil)
			for execution := 0; execution < 5; execution++ {
				test.open(breakers).policy.RecordSuccess()
			}
			for execution := 0; execution < 5; execution++ {
				test.open(breakers).policy.RecordFailure()
			}

			if !test.open(breakers).policy.IsOpen() {
				t.Fatalf("target state = %s, want open", test.open(breakers).policy.State())
			}
			if !test.untouched(breakers).policy.IsClosed() {
				t.Fatalf("untouched state = %s, want closed", test.untouched(breakers).policy.State())
			}
			metrics := test.untouched(breakers).policy.Metrics()
			if metrics.Executions() != 0 || metrics.Failures() != 0 {
				t.Fatalf("untouched metrics changed: executions=%d failures=%d", metrics.Executions(), metrics.Failures())
			}
		})
	}
}

func TestCircuitStateTransitionsAreLogged(t *testing.T) {
	var output bytes.Buffer
	breakers := NewCircuitBreakers(time.Second, log.New(&output, "", 0))
	breakers.Payment.policy.Open()
	breakers.Payment.policy.HalfOpen()
	breakers.Payment.policy.Close()

	logOutput := output.String()
	for _, expected := range []string{
		"circuit_breaker=payment old_state=closed new_state=open",
		"circuit_breaker=payment old_state=open new_state=half-open",
		"circuit_breaker=payment old_state=half-open new_state=closed",
	} {
		if !strings.Contains(logOutput, expected) {
			t.Fatalf("log does not contain %q: %s", expected, logOutput)
		}
	}
}

func TestSagaCompensatesWhenPaymentCircuitIsOpen(t *testing.T) {
	transport := &scriptedTransport{defaultStatus: http.StatusOK}
	breakers := NewCircuitBreakers(time.Second, nil)
	breakers.Payment.policy.Open()
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

	result, err := service.Create(context.Background(), CreateRequest{SKU: "SKU-001", Quantity: 1})
	assertSagaCode(t, err, "payment_circuit_open")
	if result.Status != "CANCELLED" || transport.callCount() != 0 {
		t.Fatalf("status=%s HTTP calls=%d, want CANCELLED and 0", result.Status, transport.callCount())
	}
	assertEvents(t, events, []string{})
}

func TestSagaCompensatesWhenShippingCircuitIsOpen(t *testing.T) {
	transport := &scriptedTransport{defaultStatus: http.StatusOK}
	breakers := NewCircuitBreakers(time.Second, nil)
	breakers.Shipping.policy.Open()
	events := []string{}
	repository := &fakeRepository{}
	service := NewService(
		repository,
		&fakePayment{events: &events},
		&fakeInventory{events: &events},
		NewShippingClient("http://shipping.test", &http.Client{Transport: transport}, breakers.Shipping),
		time.Second,
		nil,
	)

	result, err := service.Create(context.Background(), CreateRequest{SKU: "SKU-001", Quantity: 1})
	assertSagaCode(t, err, "shipping_circuit_open")
	if result.Status != "CANCELLED" || transport.callCount() != 0 {
		t.Fatalf("status=%s HTTP calls=%d, want CANCELLED and 0", result.Status, transport.callCount())
	}
	assertEvents(t, events, []string{"charge", "reserve", "release", "refund"})
}

func TestCompensationsBypassOpenCircuits(t *testing.T) {
	transport := &scriptedTransport{defaultStatus: http.StatusOK}
	breakers := NewCircuitBreakers(time.Second, nil)
	breakers.Payment.policy.Open()
	breakers.Shipping.policy.Open()
	client := &http.Client{Transport: transport}
	payment := NewPaymentClient("http://payment.test", client, breakers.Payment)
	shipping := NewShippingClient("http://shipping.test", client, breakers.Shipping)

	if err := payment.Refund(context.Background(), 11); err != nil {
		t.Fatalf("refund: %v", err)
	}
	if err := shipping.Cancel(context.Background(), 21); err != nil {
		t.Fatalf("cancel shipping: %v", err)
	}
	if transport.callCount() != 2 {
		t.Fatalf("compensation HTTP calls = %d, want 2", transport.callCount())
	}
	assertEvents(t, transport.paths, []string{"POST /payments/11/refund", "DELETE /shipping/21"})
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestTechnicalFailureClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "server error", err: &RemoteError{StatusCode: 503}, want: true},
		{name: "client error", err: &RemoteError{StatusCode: 409}, want: false},
		{name: "timeout", err: timeoutError{}, want: true},
		{name: "local error", err: errors.New("local failure"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isTechnicalFailure(test.err); got != test.want {
				t.Fatalf("isTechnicalFailure(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}

func assertSagaCode(t *testing.T, err error, code string) {
	t.Helper()
	var sagaError *SagaError
	if !errors.As(err, &sagaError) {
		t.Fatalf("error = %#v, want SagaError", err)
	}
	if sagaError.StatusCode != http.StatusServiceUnavailable || sagaError.Code != code || !sagaError.Compensated {
		t.Fatalf("saga error = %+v, want 503 %s compensated", sagaError, code)
	}
}
