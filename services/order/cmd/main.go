package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/MenWic/POC5-Saga_vs_Circuit-SA/services/order/internal/order"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := requiredEnvironment("DATABASE_URL")
	port := environmentOrDefault("PORT", "8080")
	paymentURL := environmentOrDefault("PAYMENT_URL", "http://localhost:8081")
	inventoryURL := environmentOrDefault("INVENTORY_URL", "http://localhost:8082")
	shippingURL := environmentOrDefault("SHIPPING_URL", "http://localhost:8083")

	connectContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(connectContext, databaseURL)
	if err != nil {
		log.Fatalf("configure database pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(connectContext); err != nil {
		log.Fatalf("connect to order database: %v", err)
	}

	httpClient := &http.Client{Timeout: 2 * time.Second}
	breakers := order.NewCircuitBreakers(10*time.Second, log.Default())
	repository := order.NewPostgresRepository(pool)
	service := order.NewService(
		repository,
		order.NewPaymentClient(paymentURL, httpClient, breakers.Payment),
		order.NewInventoryClient(inventoryURL, httpClient),
		order.NewShippingClient(shippingURL, httpClient, breakers.Shipping),
		5*time.Second,
		log.Default(),
	)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           order.NewHandler(service, breakers),
		ReadHeaderTimeout: 3 * time.Second,
	}

	log.Printf("order service listening on port %s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("order server: %v", err)
	}
}

func requiredEnvironment(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("environment variable %s is required", name)
	}
	return value
}

func environmentOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
