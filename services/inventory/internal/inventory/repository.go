package inventory

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository define la persistencia requerida por Inventory
type Repository interface {
	Reserve(context.Context, int64, string, int) (Reservation, error)
	Release(context.Context, int64) (Reservation, error)
	GetItem(context.Context, string) (Item, error)
}

// PostgresRepository persiste inventario con pgxpool
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository crea el repositorio transaccional de inventario
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Reserve descuenta stock y crea la reserva en una transacción
func (r *PostgresRepository) Reserve(ctx context.Context, orderID int64, sku string, quantity int) (Reservation, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Reservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existingStatus string
	err = tx.QueryRow(ctx, `SELECT status FROM reservations WHERE order_id = $1`, orderID).Scan(&existingStatus)
	if err == nil {
		return Reservation{}, ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, err
	}

	var available int
	err = tx.QueryRow(ctx, `SELECT available_quantity FROM items WHERE sku = $1 FOR UPDATE`, sku).Scan(&available)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrNotFound
	}
	if err != nil {
		return Reservation{}, err
	}
	if available < quantity {
		return Reservation{}, ErrInsufficientStock
	}

	if _, err = tx.Exec(ctx, `UPDATE items SET available_quantity = available_quantity - $1 WHERE sku = $2`, quantity, sku); err != nil {
		return Reservation{}, err
	}

	const insertReservation = `
		INSERT INTO reservations (order_id, sku, quantity)
		VALUES ($1, $2, $3)
		RETURNING order_id, sku, quantity, status, created_at, released_at`
	var result Reservation
	err = tx.QueryRow(ctx, insertReservation, orderID, sku, quantity).Scan(
		&result.OrderID,
		&result.SKU,
		&result.Quantity,
		&result.Status,
		&result.CreatedAt,
		&result.ReleasedAt,
	)
	if isUniqueViolation(err) {
		return Reservation{}, ErrConflict
	}
	if err != nil {
		return Reservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, err
	}
	return result, nil
}

// Release repone stock una sola vez dentro de una transacción
func (r *PostgresRepository) Release(ctx context.Context, orderID int64) (Reservation, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Reservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const selectReservation = `
		SELECT order_id, sku, quantity, status, created_at, released_at
		FROM reservations
		WHERE order_id = $1
		FOR UPDATE`
	var result Reservation
	err = tx.QueryRow(ctx, selectReservation, orderID).Scan(
		&result.OrderID,
		&result.SKU,
		&result.Quantity,
		&result.Status,
		&result.CreatedAt,
		&result.ReleasedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrNotFound
	}
	if err != nil {
		return Reservation{}, err
	}
	if result.Status == "RELEASED" {
		if err := tx.Commit(ctx); err != nil {
			return Reservation{}, err
		}
		return result, nil
	}

	// El bloqueo de la reserva garantiza que el stock se reponga una sola vez
	if _, err = tx.Exec(ctx, `UPDATE items SET available_quantity = available_quantity + $1 WHERE sku = $2`, result.Quantity, result.SKU); err != nil {
		return Reservation{}, err
	}
	const releaseReservation = `
		UPDATE reservations
		SET status = 'RELEASED', released_at = COALESCE(released_at, NOW())
		WHERE order_id = $1
		RETURNING order_id, sku, quantity, status, created_at, released_at`
	err = tx.QueryRow(ctx, releaseReservation, orderID).Scan(
		&result.OrderID,
		&result.SKU,
		&result.Quantity,
		&result.Status,
		&result.CreatedAt,
		&result.ReleasedAt,
	)
	if err != nil {
		return Reservation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Reservation{}, err
	}
	return result, nil
}

// GetItem obtiene las existencias de un SKU
func (r *PostgresRepository) GetItem(ctx context.Context, sku string) (Item, error) {
	var result Item
	err := r.pool.QueryRow(ctx, `SELECT sku, available_quantity FROM items WHERE sku = $1`, sku).Scan(
		&result.SKU,
		&result.AvailableQuantity,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return result, err
}

func isUniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
