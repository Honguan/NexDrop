package maintenance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrUnsafePath = errors.New("storage path escapes root")

type ExpiredFile struct {
	ID          string
	StoragePath string
	ChunkPaths  []string
}

type Store interface {
	ExpiredFiles(context.Context, time.Time, int) ([]ExpiredFile, error)
	MarkFileExpired(context.Context, string, time.Time) error
}

type Cleaner struct {
	store Store
	root  string
	now   func() time.Time
}

func NewCleaner(store Store, root string) (*Cleaner, error) {
	if root == "" {
		return nil, ErrUnsafePath
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve storage root: %w", err)
	}
	return &Cleaner{store: store, root: absolute, now: time.Now}, nil
}

func (cleaner *Cleaner) RunOnce(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	now := cleaner.now().UTC()
	files, err := cleaner.store.ExpiredFiles(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	cleaned := 0
	for _, file := range files {
		paths := append([]string{file.StoragePath}, file.ChunkPaths...)
		parents := make(map[string]bool)
		for _, storedPath := range paths {
			if storedPath == "" {
				continue
			}
			path, err := cleaner.safePath(storedPath)
			if err != nil {
				return cleaned, err
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return cleaned, fmt.Errorf("delete expired file: %w", err)
			}
			parents[filepath.Dir(path)] = true
		}
		for parent := range parents {
			if parent != cleaner.root {
				_ = os.Remove(parent)
			}
		}
		if err := cleaner.store.MarkFileExpired(ctx, file.ID, now); err != nil {
			return cleaned, err
		}
		cleaned++
	}
	return cleaned, nil
}

func (cleaner *Cleaner) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = cleaner.RunOnce(ctx, 100)
		}
	}
}

func (cleaner *Cleaner) safePath(storedPath string) (string, error) {
	path := storedPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(cleaner.root, path)
	}
	path = filepath.Clean(path)
	relative, err := filepath.Rel(cleaner.root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrUnsafePath
	}
	return path, nil
}
