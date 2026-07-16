package main

import (
	"context"
	"log/slog"
	"os"

	"nexdrop/internal/nativebridge"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))
	config, err := nativebridge.LoadConfig()
	if err != nil {
		fatal(err)
	}
	client, err := nativebridge.NewClient(config.URL, config.Token)
	if err != nil {
		fatal(err)
	}
	if err := nativebridge.Run(context.Background(), os.Stdin, os.Stdout, client); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	slog.Error("native bridge stopped", "module", "native_bridge", "error_code", "FATAL", "error", err)
	os.Exit(1)
}
