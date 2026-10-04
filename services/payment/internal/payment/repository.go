package payment

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository define la persistencia requerida por Payment
type Repository interface {
	Create(context.Context, int64, int64) (Payment, error)
	Get(context.Context, int64) (Payment, error)
	Refund(context.Context, int64) (Payment, error)
}

// PostgresRepository persiste pagos con pgxpool
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository crea el repositorio de pagos
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Create inserta un único cobro por orden
func (r *PostgresRepository) Create(ctx context.Context, orderID, amountCents int64) (Payment, error) {
	const query = `
		INSERT INTO payments (order_id, amount_cents)
		VALUES ($1, $2)
		RETURNING id, order_id, amount_cents, status, charged_at, refunded_at`

	var result Payment
	err := r.pool.QueryRow(ctx, query, orderID, amountCents).Scan(
		&result.ID,
		&result.OrderID,
		&result.AmountCents,
		&result.Status,
		&result.ChargedAt,
		&result.RefundedAt,
	)
	if isUniqueViolation(err) {
		return Payment{}, ErrConflict
	}
	return result, err
}

// Get obtiene un pago por identificador
func (r *PostgresRepository) Get(ctx context.Context, id int64) (Payment, error) {
	const query = `
		SELECT id, order_id, amount_cents, status, charged_at, refunded_at
		FROM payments
		WHERE id = $1`

	var result Payment
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&result.ID,
		&result.OrderID,
		&result.AmountCents,
		&result.Status,
		&result.ChargedAt,
		&result.RefundedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Payment{}, ErrNotFound
	}
	return result, err
}

// Refund cambia un cobro a reembolsado de forma idempotente
func (r *PostgresRepository) Refund(ctx context.Context, id int64) (Payment, error) {
	const query = `
		UPDATE payments
		SET status = 'REFUNDED', refunded_at = COALESCE(refunded_at, NOW())
		WHERE id = $1 AND status = 'CHARGED'
		RETURNING id, order_id, amount_cents, status, charged_at, refunded_at`

	var result Payment
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&result.ID,
		&result.OrderID,
		&result.AmountCents,
		&result.Status,
		&result.ChargedAt,
		&result.RefundedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Consultar después del UPDATE hace idempotente el segundo reembolso
		return r.Get(ctx, id)
	}
	return result, err
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
