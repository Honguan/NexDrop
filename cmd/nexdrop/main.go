package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
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
	"nexdrop/internal/pairing"
	"nexdrop/internal/postgres"
	"nexdrop/internal/presence"
	"nexdrop/internal/transfer"
	buildversion "nexdrop/internal/version"
	"nexdrop/internal/webui"
)

const defaultAddress = ":8080"

var version = buildversion.ProductVersion

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
	nodeOwner := strings.TrimSpace(os.Getenv("NEXDROP_NODE_OWNER"))
	nodeKey := strings.TrimSpace(os.Getenv("NEXDROP_NODE_KEY"))
	if nodeOwner == "" || len(nodeKey) < 32 {
		fatal("configuration failed", errors.New("NEXDROP_NODE_OWNER and a node key of at least 32 characters are required"))
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
	pairingService := pairing.NewService(store)
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
	analyticsService := analytics.NewService(store)
	adminService := admin.NewService(store)
	if err := adminService.Bootstrap(context.Background(), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_USERNAME"), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_EMAIL"), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_PASSWORD")); err != nil {
		fatal("bootstrap administrator", err)
	}
	if err := adminService.BootstrapTOTP(context.Background(), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_USERNAME"), os.Getenv("NEXDROP_BOOTSTRAP_ADMIN_TOTP_SECRET")); err != nil {
		fatal("bootstrap administrator TOTP", err)
	}
	cleaner, err := maintenance.NewCleaner(store, storagePath)
	if err != nil {
		fatal("configure cleanup worker", err)
	}
	go func() {
		_, _ = cleaner.RunOnce(context.Background(), 100)
		cleaner.Start(context.Background(), time.Hour)
	}()
	collector := monitoring.NewCollector(store, monitoring.NewSystemSampler(), storagePath)
	go func() {
		_ = collector.RunOnce(context.Background())
		collector.Start(context.Background(), 5*time.Second)
	}()
	applicationAPI := api.NewWithCursorKey([]byte(cursorSecret), authService, deviceService, pairingService, groupService, transferService, fileService, analyticsService, adminService)
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
	mux.Handle("/api/", applicationAPI.Routes())
	mux.Handle("/ws", presenceHub)
	mux.Handle("/", webHandler)

	server := &http.Server{
		Addr:              address,
		Handler:           allowedIPHandler(mux, os.Getenv("NEXDROP_ALLOWED_IPS")),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	slog.Info("NexDrop Node listening", "module", "server", "address", address, "version", version)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		fatal("HTTP server stopped", err)
	}
}

func allowedIPHandler(next http.Handler, configured string) http.Handler {
	allowed := make(map[string]struct{})
	for _, value := range strings.Split(configured, ",") {
		if value = strings.TrimSpace(value); value != "" {
			allowed[value] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			next.ServeHTTP(w, r)
			return
		}
		host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0])
		if host == "" {
			host = r.RemoteAddr
			if parsed, _, err := net.SplitHostPort(host); err == nil {
				host = parsed
			}
		}
		if _, ok := allowed[host]; !ok {
			http.Error(w, "source IP is not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func fatal(message string, err error) {
	slog.Error(message, "module", "server", "error_code", "FATAL", "error", err)
	os.Exit(1)
}

func runMaintenanceCommand(ctx context.Context, arguments []string) (bool, error) {
	if len(arguments) == 0 || arguments[0] == "serve" {
		return false, nil
	}
	if arguments[0] == "version" {
		fmt.Println(version)
		return true, nil
	}
	if arguments[0] == "totp-code" {
		flags := flag.NewFlagSet("totp-code", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		secret := flags.String("secret", "", "base32 TOTP secret")
		if err := flags.Parse(arguments[1:]); err != nil || strings.TrimSpace(*secret) == "" {
			return true, errors.New("usage: nexdrop totp-code --secret BASE32_SECRET")
		}
		code, err := auth.CurrentTOTPCode(*secret, time.Now().UTC())
		if err != nil {
			return true, err
		}
		fmt.Println(code)
		return true, nil
	}
	databaseURL := os.Getenv("NEXDROP_DATABASE_URL")
	if databaseURL == "" {
		return true, errors.New("NEXDROP_DATABASE_URL is required")
	}
	databasePassword := os.Getenv("NEXDROP_DATABASE_PASSWORD")
	databaseCommandURL, err := postgres.DatabaseURLWithPassword(databaseURL, databasePassword)
	if err != nil {
		return true, fmt.Errorf("configure PostgreSQL credentials: %w", err)
	}
	storagePath := os.Getenv("NEXDROP_STORAGE_PATH")
	if storagePath == "" {
		storagePath = "/var/lib/nexdrop"
	}
	service := backup.NewService(func(ctx context.Context, databaseURL string) (backup.SecurityStore, error) {
		return postgres.OpenWithPassword(ctx, databaseURL, databasePassword)
	})
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
		return true, service.Restore(ctx, databaseCommandURL, storagePath, *archive)
	default:
		return true, fmt.Errorf("unknown command %q", arguments[0])
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok", Version: version})
}
