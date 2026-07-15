package operations

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Database interface {
	Ping(context.Context) error
}

type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

func Doctor(ctx context.Context, database Database, storagePath string) []Check {
	checks := make([]Check, 0, 4)
	checks = append(checks, check("database", database.Ping(ctx)))
	checks = append(checks, check("storage", writable(storagePath)))
	_, dumpErr := exec.LookPath("pg_dump")
	checks = append(checks, check("pg_dump", dumpErr))
	_, restoreErr := exec.LookPath("pg_restore")
	checks = append(checks, check("pg_restore", restoreErr))
	return checks
}

func Healthy(checks []Check) bool {
	for _, item := range checks {
		if !item.OK {
			return false
		}
	}
	return true
}

func check(name string, err error) Check {
	if err != nil {
		return Check{Name: name, Detail: err.Error()}
	}
	return Check{Name: name, OK: true}
}

func writable(path string) error {
	if path == "" {
		return fmt.Errorf("storage path is empty")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(path, ".nexdrop-doctor-")
	if err != nil {
		return err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		return err
	}
	if !within(path, name) {
		return fmt.Errorf("temporary file escaped storage path")
	}
	return os.Remove(name)
}

func within(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !filepath.IsAbs(relative)
}
