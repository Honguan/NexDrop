package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nexdrop/internal/admin"
	"nexdrop/internal/analytics"
	"nexdrop/internal/api"
	"nexdrop/internal/auth"
	"nexdrop/internal/backup"
	"nexdrop/internal/device"
	"nexdrop/internal/filetransfer"
	"nexdrop/internal/group"
	"nexdrop/internal/logging"
	"nexdrop/internal/maintenance"
	"nexdrop/internal/monitoring"
	"nexdrop/internal/operations"
	"nexdrop/internal/postgres"
	"nexdrop/internal/presence"
	"nexdrop/internal/transfer"
	"nexdrop/internal/v3"
	internalversion "nexdrop/internal/version"
	"nexdrop/internal/webui"
)

const defaultAddress = ":8080"

var version = "development"

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func main() {
	slog.SetDefault(slog.New(logging.NewJSONHandler(os.Stderr, slog.LevelInfo)))
	handled, err := runMaintenanceCommand(context.Background(), os.Args[1:])
	if err != nil {
		fatal("maintenance command failed", err)
	}
	if handled {
		return
	}
	address := os.Getenv("NEXDROP_HTTP_ADDRESS")
	if address == "" {
		address = defaultAddress
	}

	databaseURL := os.Getenv("NEXDROP_DATABASE_URL")
	if databaseURL == "" {
		fatal("configuration failed", errors.New("NEXDROP_DATABASE_URL is required"))
	}
	cursorSecret := os.Getenv("NEXDROP_CURSOR_SECRET")
	if len(cursorSecret) < 32 {
		fatal("configuration failed", errors.New("NEXDROP_CURSOR_SECRET must contain at least 32 characters"))
	}
	nodeKey := strings.TrimSpace(os.Getenv("NEXDROP_NODE_KEY"))
	if len(nodeKey) < 32 {
		fatal("configuration failed", errors.New("NEXDROP_NODE_KEY must contain at least 32 characters"))
	}
	nodeID := strings.TrimSpace(os.Getenv("NEXDROP_NODE_ID"))
	if len(nodeID) < 8 {
		fatal("configuration failed", errors.New("NEXDROP_NODE_ID must contain at least 8 characters"))
	}
	store, err := postgres.OpenWithPassword(context.Background(), databaseURL, os.Getenv("NEXDROP_DATABASE_PASSWORD"))
	if err != nil {
		fatal("connect to PostgreSQL", err)
	}
	defer store.Close()
	migrationsPath := os.Getenv("NEXDROP_MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "/usr/share/nexdrop/migrations"
	}
	if err := store.ApplyMigrations(context.Background(), migrationsPath); err != nil {
		fatal("apply database migrations", err)
	}

	authService := auth.NewService(store, 15*time.Minute, 30*24*time.Hour)
	deviceService := device.NewService(store)
	groupService := group.NewService(store)
	transferService := transfer.NewService(store)
	storagePath := os.Getenv("NEXDROP_STORAGE_PATH")
	if storagePath == "" {
		storagePath = "/var/lib/nexdrop"
	}
	fileService, err := filetransfer.NewService(store, storagePath)
	if err != nil {
		fatal("configure file storage", err)
	}
	v3Service, err := v3.New(store, nodeID, []byte(nodeKey), []byte(cursorSecret), storagePath)
	if err != nil {
		fatal("configure v3 services", err)
	}
	analyticsService := analytics.NewService(store)
	adminService := admin.NewService(store)
	if err := adminService.Bootstrap(context.Background(), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_USERNAME"), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_EMAIL"), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_PASSWORD")); err != nil {
		fatal("bootstrap administrator", err)
	}
	if secret := strings.TrimSpace(os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET")); secret != "" {
		credential, err := store.CredentialByIdentifier(context.Background(), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_USERNAME"))
		if err != nil {
			fatal("load bootstrap administrator", err)
		}
		if !credential.TOTPEnabled {
			if err := store.SetTOTPSecret(context.Background(), credential.ID, secret); err != nil {
				fatal("bootstrap administrator OTP", err)
			}
		}
	}
	cleaner, err := maintenance.NewCleaner(store, storagePath)
	if err != nil {
		fatal("configure cleanup worker", err)
	}
	go func() {
		_, _ = cleaner.RunOnce(context.Background(), 100)
		cleaner.Start(context.Background(), time.Hour)
	}()
	go startRecoveryWorker(v3Service)
	go startRetentionWorker(v3Service)
	collector := monitoring.NewCollector(store, monitoring.NewSystemSampler(), storagePath)
	go func() {
		_ = collector.RunOnce(context.Background())
		collector.Start(context.Background(), 5*time.Second)
	}()
	applicationAPI := api.NewWithCursorKey([]byte(cursorSecret), authService, deviceService, groupService, transferService, fileService, analyticsService)
	presenceHub := presence.NewHub(authService, store)
	webPath := os.Getenv("NEXDROP_WEB_PATH")
	if webPath == "" {
		webPath = "/usr/share/nexdrop/web"
	}
	webHandler, err := webui.NewHandler(webPath)
	if err != nil {
		fatal("configure Web UI", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Ping(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		healthHandler(w, r)
	})
	mux.Handle("/api/v3/", applicationAPI.V3Routes(v3Service))
	mux.Handle("/api/", applicationAPI.Routes())
	mux.Handle("/metrics", monitoring.DefaultRegistry)
	mux.Handle("/ws", presenceHub)
	mux.Handle("/", webHandler)

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	slog.Info("NexDrop Node listening", "module", "server", "address", address, "version", version)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		fatal("HTTP server stopped", err)
	}
}

func startRecoveryWorker(service *v3.Service) {
	ctx := context.Background()
	if report, err := service.RunRecovery(ctx, "startup", 100); err != nil {
		slog.Error("startup transfer recovery failed", "module", "recovery", "error_code", "RECOVERY_FAILED", "error", err)
	} else {
		slog.Info("startup transfer recovery completed", "module", "recovery", "scanned", report.Scanned, "completed", report.Completed, "waiting", report.Waiting, "dead_letters", report.DeadLetters)
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if _, err := service.RunRecovery(ctx, "periodic", 100); err != nil {
			slog.Error("periodic transfer recovery failed", "module", "recovery", "error_code", "RECOVERY_FAILED", "error", err)
		}
	}
}

func startRetentionWorker(service *v3.Service) {
	ctx := context.Background()
	if _, err := service.RunRetention(ctx, 100); err != nil {
		slog.Error("startup retention cleanup failed", "module", "retention", "error_code", "RETENTION_FAILED", "error", err)
	}
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if _, err := service.RunRetention(ctx, 100); err != nil {
			slog.Error("periodic retention cleanup failed", "module", "retention", "error_code", "RETENTION_FAILED", "error", err)
		}
	}
}

func fatal(message string, err error) {
	slog.Error(message, "module", "server", "error_code", "FATAL", "error", err)
	os.Exit(1)
}

func openV3MaintenanceService(ctx context.Context, databaseURL, databasePassword, storagePath string) (*postgres.Store, *v3.Service, error) {
	store, err := postgres.OpenWithPassword(ctx, databaseURL, databasePassword)
	if err != nil {
		return nil, nil, err
	}
	migrationsPath := os.Getenv("NEXDROP_MIGRATIONS_PATH")
	if migrationsPath == "" {
		migrationsPath = "/usr/share/nexdrop/migrations"
	}
	if err := store.ApplyMigrations(ctx, migrationsPath); err != nil {
		store.Close()
		return nil, nil, err
	}
	service, err := v3.New(
		store,
		strings.TrimSpace(os.Getenv("NEXDROP_NODE_ID")),
		[]byte(strings.TrimSpace(os.Getenv("NEXDROP_NODE_KEY"))),
		[]byte(os.Getenv("NEXDROP_CURSOR_SECRET")),
		storagePath,
	)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	return store, service, nil
}

func runMaintenanceCommand(ctx context.Context, arguments []string) (bool, error) {
	if len(arguments) == 0 || arguments[0] == "serve" {
		return false, nil
	}
	if arguments[0] == "version" {
		fmt.Println(version)
		return true, nil
	}
	databaseURL := os.Getenv("NEXDROP_DATABASE_URL")
	if databaseURL == "" && arguments[0] != "diagnostics" {
		return true, errors.New("NEXDROP_DATABASE_URL is required")
	}
	databasePassword := os.Getenv("NEXDROP_DATABASE_PASSWORD")
	storagePath := os.Getenv("NEXDROP_STORAGE_PATH")
	if storagePath == "" {
		storagePath = "/var/lib/nexdrop"
	}
	switch arguments[0] {
	case "status":
		store, err := postgres.OpenWithPassword(ctx, databaseURL, databasePassword)
		if err != nil {
			return true, err
		}
		defer store.Close()
		if err := store.Ping(ctx); err != nil {
			return true, err
		}
		return true, json.NewEncoder(os.Stdout).Encode(healthResponse{Status: "ok", Version: version})
	case "doctor":
		store, err := postgres.OpenWithPassword(ctx, databaseURL, databasePassword)
		if err != nil {
			return true, err
		}
		defer store.Close()
		checks := operations.Doctor(ctx, store, storagePath)
		if err := json.NewEncoder(os.Stdout).Encode(checks); err != nil {
			return true, err
		}
		if !operations.Healthy(checks) {
			return true, errors.New("one or more checks failed")
		}
		return true, nil
	case "diagnostics":
		flags := flag.NewFlagSet("diagnostics", flag.ContinueOnError)
		output := flags.String("output", "diagnostics.zip", "diagnostics ZIP output path, or - for stdout")
		if err := flags.Parse(arguments[1:]); err != nil {
			return true, err
		}
		if flags.NArg() != 0 || strings.TrimSpace(*output) == "" {
			return true, errors.New("usage: diagnostics [--output diagnostics.zip|-]")
		}
		store, err := postgres.OpenWithPassword(ctx, databaseURL, databasePassword)
		var checks []operations.Check
		if err != nil {
			checks = operations.InspectUnavailable(storagePath, err)
		} else {
			defer store.Close()
			checks = operations.Inspect(ctx, store, storagePath)
		}
		environment := make(map[string]string)
		for _, entry := range os.Environ() {
			key, value, found := strings.Cut(entry, "=")
			if found && (strings.HasPrefix(key, "NEXDROP_") || strings.HasPrefix(key, "POSTGRES_")) {
				environment[key] = value
			}
		}
		options := operations.DiagnosticsOptions{
			Version:     version,
			Commit:      internalversion.BuildCommit,
			GeneratedAt: time.Now().UTC(),
			Checks:      checks,
			Environment: environment,
		}
		if *output == "-" {
			return true, operations.WriteDiagnostics(ctx, os.Stdout, options)
		}
		file, err := os.OpenFile(*output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return true, err
		}
		writeErr := operations.WriteDiagnostics(ctx, file, options)
		closeErr := file.Close()
		if writeErr != nil {
			return true, writeErr
		}
		return true, closeErr
	case "cleanup":
		flags := flag.NewFlagSet("cleanup", flag.ContinueOnError)
		limit := flags.Int("limit", 100, "maximum files to clean")
		if err := flags.Parse(arguments[1:]); err != nil {
			return true, err
		}
		store, err := postgres.OpenWithPassword(ctx, databaseURL, databasePassword)
		if err != nil {
			return true, err
		}
		defer store.Close()
		cleaner, err := maintenance.NewCleaner(store, storagePath)
		if err != nil {
			return true, err
		}
		cleaned, err := cleaner.RunOnce(ctx, *limit)
		if err != nil {
			return true, err
		}
		return true, json.NewEncoder(os.Stdout).Encode(map[string]int{"cleaned": cleaned})
	case "transfers":
		if len(arguments) < 2 {
			return true, errors.New("usage: transfers {failed|inspect <transfer-id>|retry <transfer-id>|reconcile [--limit N]}")
		}
		store, service, err := openV3MaintenanceService(ctx, databaseURL, databasePassword, storagePath)
		if err != nil {
			return true, err
		}
		defer store.Close()
		operator := auth.Session{User: auth.User{ID: "cli-operator", Admin: true}, SessionID: "cli", AdminVerified: true}
		switch arguments[1] {
		case "failed":
			flags := flag.NewFlagSet("transfers failed", flag.ContinueOnError)
			limit := flags.Int("limit", 100, "maximum dead-letter entries")
			if err := flags.Parse(arguments[2:]); err != nil {
				return true, err
			}
			items, err := service.FailedRecovery(ctx, operator, *limit)
			if err != nil {
				return true, err
			}
			return true, json.NewEncoder(os.Stdout).Encode(map[string]any{"items": items})
		case "inspect":
			if len(arguments) != 3 {
				return true, errors.New("usage: transfers inspect <transfer-id>")
			}
			items, err := service.InspectRecovery(ctx, operator, arguments[2])
			if err != nil {
				return true, err
			}
			return true, json.NewEncoder(os.Stdout).Encode(map[string]any{"items": items})
		case "retry":
			if len(arguments) != 3 {
				return true, errors.New("usage: transfers retry <transfer-id>")
			}
			return true, service.RetryRecovery(ctx, operator, arguments[2])
		case "reconcile":
			flags := flag.NewFlagSet("transfers reconcile", flag.ContinueOnError)
			limit := flags.Int("limit", 100, "maximum targets to reconcile")
			if err := flags.Parse(arguments[2:]); err != nil {
				return true, err
			}
			report, err := service.RunRecovery(ctx, "operator-cli", *limit)
			if err != nil {
				return true, err
			}
			return true, json.NewEncoder(os.Stdout).Encode(report)
		default:
			return true, fmt.Errorf("unknown transfers command %q", arguments[1])
		}
	case "reset-password":
		flags := flag.NewFlagSet("reset-password", flag.ContinueOnError)
		identifier := flags.String("identifier", "", "username or email")
		if err := flags.Parse(arguments[1:]); err != nil {
			return true, err
		}
		password, err := io.ReadAll(io.LimitReader(os.Stdin, 4097))
		if err != nil {
			return true, err
		}
		if len(password) > 4096 {
			return true, errors.New("password input is too long")
		}
		store, err := postgres.OpenWithPassword(ctx, databaseURL, databasePassword)
		if err != nil {
			return true, err
		}
		defer store.Close()
		return true, admin.NewService(store).ResetPasswordByIdentifier(ctx, *identifier, strings.TrimRight(string(password), "\r\n"))
	case "backup":
		flags := flag.NewFlagSet("backup", flag.ContinueOnError)
		output := flags.String("output", "", "backup archive path")
		includeFiles := flags.Bool("include-files", false, "include cached file content")
		if err := flags.Parse(arguments[1:]); err != nil {
			return true, err
		}
		if *output == "" {
			*output = filepath.Join(storagePath, "backups", "nexdrop-"+time.Now().UTC().Format("20060102T150405Z")+".tar.gz")
		}
		databaseCommandURL, err := postgres.DatabaseURLWithPassword(databaseURL, databasePassword)
		if err != nil {
			return true, fmt.Errorf("configure PostgreSQL credentials: %w", err)
		}
		service := backup.NewService(func(ctx context.Context, databaseURL string) (backup.SecurityStore, error) {
			return postgres.OpenWithPassword(ctx, databaseURL, databasePassword)
		})
		if err := service.Create(ctx, databaseCommandURL, storagePath, *output, *includeFiles); err != nil {
			return true, err
		}
		fmt.Println(*output)
		return true, nil
	case "restore":
		flags := flag.NewFlagSet("restore", flag.ContinueOnError)
		archive := flags.String("file", "", "backup archive path")
		confirmed := flags.Bool("confirm", false, "confirm destructive restore")
		if err := flags.Parse(arguments[1:]); err != nil {
			return true, err
		}
		if *archive == "" || !*confirmed {
			return true, errors.New("restore requires --file and --confirm")
		}
		databaseCommandURL, err := postgres.DatabaseURLWithPassword(databaseURL, databasePassword)
		if err != nil {
			return true, fmt.Errorf("configure PostgreSQL credentials: %w", err)
		}
		service := backup.NewService(func(ctx context.Context, databaseURL string) (backup.SecurityStore, error) {
			return postgres.OpenWithPassword(ctx, databaseURL, databasePassword)
		})
		return true, service.Restore(ctx, databaseCommandURL, storagePath, *archive)
	default:
		return true, fmt.Errorf("unknown command %q", arguments[0])
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: version})
}
