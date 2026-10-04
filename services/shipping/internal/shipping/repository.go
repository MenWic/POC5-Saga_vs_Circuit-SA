package shipping

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository define la persistencia requerida por Shipping
type Repository interface {
	Schedule(context.Context, int64) (Shipment, error)
	Get(context.Context, int64) (Shipment, error)
	Cancel(context.Context, int64) (Shipment, error)
}

// PostgresRepository persiste envíos con pgxpool
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository crea el repositorio de envíos
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Schedule crea un único envío por orden
func (r *PostgresRepository) Schedule(ctx context.Context, orderID int64) (Shipment, error) {
	const query = `
		INSERT INTO shipments (order_id)
		VALUES ($1)
		RETURNING id, order_id, status, scheduled_at, cancelled_at`
	var result Shipment
	err := r.pool.QueryRow(ctx, query, orderID).Scan(
		&result.ID,
		&result.OrderID,
		&result.Status,
		&result.ScheduledAt,
		&result.CancelledAt,
	)
	if isUniqueViolation(err) {
		return Shipment{}, ErrConflict
	}
	return result, err
}

// Get obtiene un envío por identificador
func (r *PostgresRepository) Get(ctx context.Context, id int64) (Shipment, error) {
	const query = `
		SELECT id, order_id, status, scheduled_at, cancelled_at
		FROM shipments
		WHERE id = $1`
	var result Shipment
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&result.ID,
		&result.OrderID,
		&result.Status,
		&result.ScheduledAt,
		&result.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Shipment{}, ErrNotFound
	}
	return result, err
}

// Cancel cambia un envío a cancelado de forma idempotente
func (r *PostgresRepository) Cancel(ctx context.Context, id int64) (Shipment, error) {
	const query = `
		UPDATE shipments
		SET status = 'CANCELLED', cancelled_at = COALESCE(cancelled_at, NOW())
		WHERE id = $1 AND status = 'SCHEDULED'
		RETURNING id, order_id, status, scheduled_at, cancelled_at`
	var result Shipment
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&result.ID,
		&result.OrderID,
		&result.Status,
		&result.ScheduledAt,
		&result.CancelledAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Consultar después del UPDATE hace idempotente la segunda cancelación
		return r.Get(ctx, id)
	}
	return result, err
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
