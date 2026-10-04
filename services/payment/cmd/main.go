package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/MenWic/POC5-Saga_vs_Circuit-SA/services/payment/internal/payment"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := requiredEnvironment("DATABASE_URL")
	port := environmentOrDefault("PORT", "8081")
	demoMode := os.Getenv("DEMO_MODE") == "true"

	connectContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(connectContext, databaseURL)
	if err != nil {
		log.Fatalf("configure database pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(connectContext); err != nil {
		log.Fatalf("connect to payment database: %v", err)
	}

	repository := payment.NewPostgresRepository(pool)
	service := payment.NewService(repository, demoMode)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           payment.NewHandler(service, demoMode),
		ReadHeaderTimeout: 3 * time.Second,
	}

	log.Printf("payment service listening on port %s demo_mode=%t", port, demoMode)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("payment server: %v", err)
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
