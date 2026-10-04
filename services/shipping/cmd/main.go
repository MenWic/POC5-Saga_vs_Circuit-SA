package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/MenWic/POC5-Saga_vs_Circuit-SA/services/shipping/internal/shipping"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := requiredEnvironment("DATABASE_URL")
	port := environmentOrDefault("PORT", "8083")
	demoMode := os.Getenv("DEMO_MODE") == "true"

	connectContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(connectContext, databaseURL)
	if err != nil {
		log.Fatalf("configure database pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(connectContext); err != nil {
		log.Fatalf("connect to shipping database: %v", err)
	}

	repository := shipping.NewPostgresRepository(pool)
	service := shipping.NewService(repository, demoMode)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           shipping.NewHandler(service, demoMode),
		ReadHeaderTimeout: 3 * time.Second,
	}

	log.Printf("shipping service listening on port %s demo_mode=%t", port, demoMode)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("shipping server: %v", err)
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
