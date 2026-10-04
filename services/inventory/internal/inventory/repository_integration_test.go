package inventory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresReserveReleaseAtomicity(t *testing.T) {
	databaseURL := os.Getenv("INVENTORY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("INVENTORY_TEST_DATABASE_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}

	suffix := time.Now().UnixNano()
	sku := fmt.Sprintf("TEST-%d", suffix)
	orderID := suffix
	if _, err := pool.Exec(ctx, `INSERT INTO items (sku, available_quantity) VALUES ($1, 5)`, sku); err != nil {
		t.Fatalf("insert fixture item: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, `DELETE FROM reservations WHERE order_id = $1`, orderID)
		_, _ = pool.Exec(cleanupContext, `DELETE FROM items WHERE sku = $1`, sku)
	})

	repository := NewPostgresRepository(pool)
	if _, err := repository.Reserve(ctx, orderID, sku, 6); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("reserve error = %v, want ErrInsufficientStock", err)
	}
	item, err := repository.GetItem(ctx, sku)
	if err != nil {
		t.Fatalf("get item after rejected reserve: %v", err)
	}
	if item.AvailableQuantity != 5 {
		t.Fatalf("stock after rejected reserve = %d, want 5", item.AvailableQuantity)
	}

	if _, err := repository.Reserve(ctx, orderID, sku, 2); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if _, err := repository.Release(ctx, orderID); err != nil {
		t.Fatalf("first release: %v", err)
	}
	if _, err := repository.Release(ctx, orderID); err != nil {
		t.Fatalf("second release: %v", err)
	}
	item, err = repository.GetItem(ctx, sku)
	if err != nil {
		t.Fatalf("get item after releases: %v", err)
	}
	if item.AvailableQuantity != 5 {
		t.Fatalf("stock after repeated release = %d, want 5", item.AvailableQuantity)
	}
}
