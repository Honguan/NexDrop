package main

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"nexdrop/internal/logging"
	"nexdrop/internal/relay"
)

var version = "development"

func main() {
	slog.SetDefault(slog.New(logging.NewJSONHandler(os.Stderr, slog.LevelInfo)))
	if len(os.Args) > 1 && os.Args[1] == "version" {
		_, _ = os.Stdout.WriteString(version + "\n")
		return
	}
	config, address, err := configuration()
	if err != nil {
		slog.Error("relay configuration failed", "module", "relay", "error_code", "INVALID_CONFIGURATION", "error", err)
		os.Exit(1)
	}
	server, err := relay.NewServer(config)
	if err != nil {
		slog.Error("relay storage failed", "module", "relay", "error_code", "STORAGE_UNAVAILABLE", "error", err)
		os.Exit(1)
	}
	go server.StartCleanup(context.Background().Done(), time.Hour)
	httpServer := &http.Server{
		Addr:              address,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	slog.Info("NexDrop Relay listening", "module", "relay", "address", address, "relay_id", os.Getenv("NEXDROP_RELAY_ID"), "version", version)
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("relay HTTP server stopped", "module", "relay", "error_code", "FATAL", "error", err)
		os.Exit(1)
	}
}

func configuration() (relay.ServerConfig, string, error) {
	publicKey, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(os.Getenv("NEXDROP_RELAY_GRANT_PUBLIC_KEY")))
	if err != nil {
		return relay.ServerConfig{}, "", errors.New("NEXDROP_RELAY_GRANT_PUBLIC_KEY must be base64url Ed25519 public key")
	}
	verifier, err := relay.NewGrantVerifier(publicKey)
	if err != nil {
		return relay.ServerConfig{}, "", err
	}
	capacity, err := strconv.ParseInt(strings.TrimSpace(os.Getenv("NEXDROP_RELAY_CAPACITY_BYTES")), 10, 64)
	if err != nil || capacity <= 0 {
		return relay.ServerConfig{}, "", errors.New("NEXDROP_RELAY_CAPACITY_BYTES must be positive")
	}
	retention := 7 * 24 * time.Hour
	if raw := strings.TrimSpace(os.Getenv("NEXDROP_RELAY_RETENTION")); raw != "" {
		retention, err = time.ParseDuration(raw)
		if err != nil || retention <= 0 {
			return relay.ServerConfig{}, "", errors.New("NEXDROP_RELAY_RETENTION must be a positive duration")
		}
	}
	address := strings.TrimSpace(os.Getenv("NEXDROP_RELAY_HTTP_ADDRESS"))
	if address == "" {
		address = ":8081"
	}
	root := strings.TrimSpace(os.Getenv("NEXDROP_RELAY_STORAGE_PATH"))
	if root == "" {
		root = "/var/lib/nexdrop-relay"
	}
	return relay.ServerConfig{
		RelayID: strings.TrimSpace(os.Getenv("NEXDROP_RELAY_ID")),
		StorageRoot: root,
		CapacityBytes: capacity,
		Retention: retention,
		Verifier: verifier,
	}, address, nil
}
