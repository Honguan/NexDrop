package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"nexdrop/internal/api"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/group"
	"nexdrop/internal/pairing"
	"nexdrop/internal/postgres"
	"nexdrop/internal/transfer"
)

const defaultAddress = ":8080"

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func main() {
	address := os.Getenv("NEXDROP_HTTP_ADDRESS")
	if address == "" {
		address = defaultAddress
	}

	databaseURL := os.Getenv("NEXDROP_DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("NEXDROP_DATABASE_URL is required")
	}
	store, err := postgres.Open(context.Background(), databaseURL)
	if err != nil {
		log.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer store.Close()

	authService := auth.NewService(store, 15*time.Minute, 30*24*time.Hour)
	deviceService := device.NewService(store)
	pairingService := pairing.NewService(store)
	groupService := group.NewService(store)
	transferService := transfer.NewService(store)
	applicationAPI := api.New(authService, deviceService, pairingService, groupService, transferService)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		healthHandler(w, r)
	})
	mux.Handle("/api/", applicationAPI.Routes())

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("NexDrop Node listening on %s", address)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: "v1"})
}
