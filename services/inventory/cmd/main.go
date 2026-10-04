package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/MenWic/POC5-Saga_vs_Circuit-SA/services/inventory/internal/inventory"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := requiredEnvironment("DATABASE_URL")
	port := environmentOrDefault("PORT", "8082")

	connectContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(connectContext, databaseURL)
	if err != nil {
		log.Fatalf("configure database pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(connectContext); err != nil {
		log.Fatalf("connect to inventory database: %v", err)
	}

	repository := inventory.NewPostgresRepository(pool)
	service := inventory.NewService(repository)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           inventory.NewHandler(service),
		ReadHeaderTimeout: 3 * time.Second,
	}

	log.Printf("inventory service listening on port %s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("inventory server: %v", err)
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
