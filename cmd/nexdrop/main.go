package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"nexdrop/internal/admin"
	"nexdrop/internal/analytics"
	"nexdrop/internal/api"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/filetransfer"
	"nexdrop/internal/group"
	"nexdrop/internal/maintenance"
	"nexdrop/internal/monitoring"
	"nexdrop/internal/pairing"
	"nexdrop/internal/postgres"
	"nexdrop/internal/presence"
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
	storagePath := os.Getenv("NEXDROP_STORAGE_PATH")
	if storagePath == "" {
		storagePath = "/var/lib/nexdrop"
	}
	fileService, err := filetransfer.NewService(store, storagePath)
	if err != nil {
		log.Fatalf("configure file storage: %v", err)
	}
	analyticsService := analytics.NewService(store)
	adminService := admin.NewService(store)
	if err := adminService.Bootstrap(context.Background(), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_USERNAME"), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_EMAIL"), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_PASSWORD")); err != nil {
		log.Fatalf("bootstrap administrator: %v", err)
	}
	cleaner, err := maintenance.NewCleaner(store, storagePath)
	if err != nil {
		log.Fatalf("configure cleanup worker: %v", err)
	}
	go func() {
		_, _ = cleaner.RunOnce(context.Background(), 100)
		cleaner.Start(context.Background(), time.Hour)
	}()
	collector := monitoring.NewCollector(store, monitoring.NewSystemSampler(), storagePath)
	go func() {
		_ = collector.RunOnce(context.Background())
		collector.Start(context.Background(), time.Minute)
	}()
	applicationAPI := api.New(authService, deviceService, pairingService, groupService, transferService, fileService, analyticsService, adminService)
	presenceHub := presence.NewHub(authService, store)
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
	mux.Handle("/ws", presenceHub)

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
