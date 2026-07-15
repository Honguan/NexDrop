package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"nexdrop/internal/desktopbridge"
	"nexdrop/internal/nativebridge"
)

const bridgeAddress = "127.0.0.1:41739"

type spoolQueue struct{ directory string }

func (queue spoolQueue) Enqueue(_ context.Context, payload nativebridge.SharePayload) (string, error) {
	if err := os.MkdirAll(queue.directory, 0o700); err != nil {
		return "", err
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	content, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	temporary := filepath.Join(queue.directory, "."+id+".tmp")
	destination := filepath.Join(queue.directory, id+".json")
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	return id, nil
}

type desktopStatus struct{ path string }

func (status desktopStatus) Status(_ context.Context) (json.RawMessage, error) {
	content, err := os.ReadFile(status.path)
	if errors.Is(err, os.ErrNotExist) {
		return json.RawMessage(`{"online":true}`), nil
	}
	if err != nil || !json.Valid(content) {
		return nil, errors.New("invalid desktop status")
	}
	return content, nil
}

func main() {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		log.Fatal("LOCALAPPDATA is required")
	}
	root = filepath.Join(root, "NexDrop")
	origin := os.Getenv("NEXDROP_WEB_ORIGIN")
	if origin == "" {
		origin = "http://localhost:8080"
	}
	service, err := desktopbridge.New(origin, spoolQueue{filepath.Join(root, "bridge-queue")}, desktopStatus{filepath.Join(root, "status.json")})
	if err != nil {
		log.Fatal(err)
	}
	token, err := service.IssueNativeToken()
	if err != nil {
		log.Fatal(err)
	}
	listener, err := net.Listen("tcp4", bridgeAddress)
	if err != nil {
		log.Fatal(err)
	}
	if err := writeConfig(root, token); err != nil {
		_ = listener.Close()
		log.Fatal(err)
	}
	server := &http.Server{
		Handler:           service.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = server.Shutdown(shutdown)
	}()
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func writeConfig(root, token string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	content, err := json.Marshal(nativebridge.Config{URL: "http://" + bridgeAddress, Token: token})
	if err != nil {
		return err
	}
	temporary := filepath.Join(root, "bridge.json.tmp")
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	destination := filepath.Join(root, "bridge.json")
	_ = os.Remove(destination)
	return os.Rename(temporary, destination)
}
