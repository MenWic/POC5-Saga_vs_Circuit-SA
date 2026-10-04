package order

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository define la persistencia requerida por Order
type Repository interface {
	Create(context.Context, string, int, int64) (Order, error)
	Get(context.Context, int64) (Order, error)
	SetPaymentID(context.Context, int64, int64) error
	SetShippingID(context.Context, int64, int64) error
	Complete(context.Context, int64) (Order, error)
	MarkFailed(context.Context, int64, string, string) (Order, error)
	Cancel(context.Context, int64, string) (Order, error)
}

// PostgresRepository persiste órdenes con pgxpool
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository crea el repositorio de órdenes
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create inserta una orden pendiente
func (r *PostgresRepository) Create(ctx context.Context, sku string, quantity int, amountCents int64) (Order, error) {
	const query = `
		INSERT INTO orders (sku, quantity, amount_cents)
		VALUES ($1, $2, $3)
		RETURNING id, sku, quantity, amount_cents, status, payment_id, shipping_id, failure_reason, created_at, updated_at`
	return scanOrder(r.pool.QueryRow(ctx, query, sku, quantity, amountCents))
}

// Get obtiene una orden por identificador
func (r *PostgresRepository) Get(ctx context.Context, id int64) (Order, error) {
	const query = `
		SELECT id, sku, quantity, amount_cents, status, payment_id, shipping_id, failure_reason, created_at, updated_at
		FROM orders
		WHERE id = $1`
	return scanOrder(r.pool.QueryRow(ctx, query, id))
}

// SetPaymentID guarda la referencia del cobro completado
func (r *PostgresRepository) SetPaymentID(ctx context.Context, id, paymentID int64) error {
	command, err := r.pool.Exec(ctx, `UPDATE orders SET payment_id = $1, updated_at = NOW() WHERE id = $2`, paymentID, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetShippingID guarda la referencia del envío programado
func (r *PostgresRepository) SetShippingID(ctx context.Context, id, shippingID int64) error {
	command, err := r.pool.Exec(ctx, `UPDATE orders SET shipping_id = $1, updated_at = NOW() WHERE id = $2`, shippingID, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Complete confirma una orden pendiente
func (r *PostgresRepository) Complete(ctx context.Context, id int64) (Order, error) {
	const query = `
		UPDATE orders
		SET status = 'COMPLETED', failure_reason = NULL, updated_at = NOW()
		WHERE id = $1 AND status = 'PENDING'
		RETURNING id, sku, quantity, amount_cents, status, payment_id, shipping_id, failure_reason, created_at, updated_at`
	return scanOrder(r.pool.QueryRow(ctx, query, id))
}

// MarkFailed registra el resultado de una Saga fallida
func (r *PostgresRepository) MarkFailed(ctx context.Context, id int64, status, reason string) (Order, error) {
	const query = `
		UPDATE orders
		SET status = $1, failure_reason = $2, updated_at = NOW()
		WHERE id = $3
		RETURNING id, sku, quantity, amount_cents, status, payment_id, shipping_id, failure_reason, created_at, updated_at`
	return scanOrder(r.pool.QueryRow(ctx, query, status, reason, id))
}

// Cancel aplica la cancelación lógica local
func (r *PostgresRepository) Cancel(ctx context.Context, id int64, reason string) (Order, error) {
	const query = `
		UPDATE orders
		SET status = 'CANCELLED', failure_reason = $1, updated_at = NOW()
		WHERE id = $2 AND status = 'PENDING'
		RETURNING id, sku, quantity, amount_cents, status, payment_id, shipping_id, failure_reason, created_at, updated_at`
	result, err := scanOrder(r.pool.QueryRow(ctx, query, reason, id))
	if !errors.Is(err, ErrNotFound) {
		return result, err
	}

	current, err := r.Get(ctx, id)
	if err != nil {
		return Order{}, err
	}
	if current.Status == "CANCELLED" {
		return current, nil
	}
	return Order{}, ErrCancellationConflict
}

type rowScanner interface {
	Scan(...any) error
}

func scanOrder(row rowScanner) (Order, error) {
	var result Order
	err := row.Scan(
		&result.ID,
		&result.SKU,
		&result.Quantity,
		&result.AmountCents,
		&result.Status,
		&result.PaymentID,
		&result.ShippingID,
		&result.FailureReason,
		&result.CreatedAt,
		&result.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrNotFound
	}
	return result, err
}
