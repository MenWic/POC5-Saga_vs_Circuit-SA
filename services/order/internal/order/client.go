package order

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/circuitbreaker"
)

// PaymentGateway define cobro y reembolso remotos
type PaymentGateway interface {
	Charge(context.Context, int64, int64) (Payment, error)
	Refund(context.Context, int64) error
}

// InventoryGateway define reserva y liberación remotas
type InventoryGateway interface {
	Reserve(context.Context, int64, string, int) error
	Release(context.Context, int64) error
}

// ShippingGateway define programación y cancelación remotas
type ShippingGateway interface {
	Schedule(context.Context, int64) (Shipment, error)
	Cancel(context.Context, int64) error
}

// PaymentClient implementa PaymentGateway por HTTP
type PaymentClient struct {
	baseURL string
	client  *http.Client
	breaker *Breaker
}

// InventoryClient implementa InventoryGateway por HTTP
type InventoryClient struct {
	baseURL string
	client  *http.Client
}

// ShippingClient implementa ShippingGateway por HTTP
type ShippingClient struct {
	baseURL string
	client  *http.Client
	breaker *Breaker
}

// NewPaymentClient crea el cliente HTTP de Payment
func NewPaymentClient(baseURL string, client *http.Client, breaker *Breaker) *PaymentClient {
	return &PaymentClient{baseURL: strings.TrimRight(baseURL, "/"), client: client, breaker: breaker}
}

// NewInventoryClient crea el cliente HTTP de Inventory
func NewInventoryClient(baseURL string, client *http.Client) *InventoryClient {
	return &InventoryClient{baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

// NewShippingClient crea el cliente HTTP de Shipping
func NewShippingClient(baseURL string, client *http.Client, breaker *Breaker) *ShippingClient {
	return &ShippingClient{baseURL: strings.TrimRight(baseURL, "/"), client: client, breaker: breaker}
}

// Charge solicita el cobro principal
func (c *PaymentClient) Charge(ctx context.Context, orderID, amountCents int64) (Payment, error) {
	result, err := failsafe.WithAny[Payment](c.breaker.policy).WithContext(ctx).Get(func() (Payment, error) {
		var payment Payment
		err := sendJSON(ctx, c.client, http.MethodPost, c.baseURL+"/payments", map[string]any{
			"order_id":     orderID,
			"amount_cents": amountCents,
		}, &payment, "payment")
		return payment, err
	})
	if errors.Is(err, circuitbreaker.ErrOpen) {
		return Payment{}, circuitOpenError("payment")
	}
	return result, err
}

// Refund solicita la compensación del cobro
func (c *PaymentClient) Refund(ctx context.Context, paymentID int64) error {
	return sendJSON(ctx, c.client, http.MethodPost, fmt.Sprintf("%s/payments/%d/refund", c.baseURL, paymentID), nil, nil, "payment")
}

// Reserve solicita una reserva de stock
func (c *InventoryClient) Reserve(ctx context.Context, orderID int64, sku string, quantity int) error {
	return sendJSON(ctx, c.client, http.MethodPost, c.baseURL+"/inventory/reserve", map[string]any{
		"order_id": orderID,
		"sku":      sku,
		"quantity": quantity,
	}, nil, "inventory")
}

// Release solicita la compensación del inventario
func (c *InventoryClient) Release(ctx context.Context, orderID int64) error {
	return sendJSON(ctx, c.client, http.MethodPost, c.baseURL+"/inventory/release", map[string]any{
		"order_id": orderID,
	}, nil, "inventory")
}

// Schedule solicita la programación del envío
func (c *ShippingClient) Schedule(ctx context.Context, orderID int64) (Shipment, error) {
	result, err := failsafe.WithAny[Shipment](c.breaker.policy).WithContext(ctx).Get(func() (Shipment, error) {
		var shipment Shipment
		err := sendJSON(ctx, c.client, http.MethodPost, c.baseURL+"/shipping/schedule", map[string]any{
			"order_id": orderID,
		}, &shipment, "shipping")
		return shipment, err
	})
	if errors.Is(err, circuitbreaker.ErrOpen) {
		return Shipment{}, circuitOpenError("shipping")
	}
	return result, err
}

// Cancel solicita la compensación del envío
func (c *ShippingClient) Cancel(ctx context.Context, shippingID int64) error {
	return sendJSON(ctx, c.client, http.MethodDelete, fmt.Sprintf("%s/shipping/%d", c.baseURL, shippingID), nil, nil, "shipping")
}

func circuitOpenError(service string) *RemoteError {
	return &RemoteError{
		StatusCode: http.StatusServiceUnavailable,
		Code:       service + "_circuit_open",
		Message:    service + " circuit breaker is open",
		Service:    service,
	}
}

func sendJSON(ctx context.Context, client *http.Client, method, url string, input, output any, service string) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.Do(request)
	if err != nil {
		return &RemoteError{StatusCode: http.StatusServiceUnavailable, Code: service + "_unavailable", Message: err.Error(), Service: service}
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var remoteBody struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&remoteBody)
		if remoteBody.Code == "" {
			remoteBody.Code = service + "_error"
		}
		if remoteBody.Message == "" {
			remoteBody.Message = response.Status
		}
		return &RemoteError{
			StatusCode: response.StatusCode,
			Code:       remoteBody.Code,
			Message:    remoteBody.Message,
			Service:    service,
		}
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output); err != nil {
		return &RemoteError{StatusCode: http.StatusServiceUnavailable, Code: service + "_invalid_response", Message: "invalid response body", Service: service}
	}
	return nil
}
