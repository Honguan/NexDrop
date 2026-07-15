package main

import (
	"context"
	"log"
	"os"

	"nexdrop/internal/nativebridge"
)

func main() {
	log.SetOutput(os.Stderr)
	config, err := nativebridge.LoadConfig()
	if err != nil {
		log.Fatal(err)
	}
	client, err := nativebridge.NewClient(config.URL, config.Token)
	if err != nil {
		log.Fatal(err)
	}
	if err := nativebridge.Run(context.Background(), os.Stdin, os.Stdout, client); err != nil {
		log.Fatal(err)
	}
}
