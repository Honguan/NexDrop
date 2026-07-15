package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const FormatVersion = 1

var ErrInvalidArchive = errors.New("invalid backup archive")

type Manifest struct {
	FormatVersion int       `json:"formatVersion"`
	CreatedAt     time.Time `json:"createdAt"`
	IncludeFiles  bool      `json:"includeFiles"`
}

type Runner interface {
	Run(context.Context, string, ...string) error
}

type SecurityStore interface {
	RevokedDeviceIDs(context.Context) ([]string, error)
	ProtectRestoredSecurity(context.Context, []string, time.Time) error
	Close()
}

type StoreOpener func(context.Context, string) (SecurityStore, error)

type Service struct {
	runner Runner
	open   StoreOpener
	now    func() time.Time
}

func NewService(open StoreOpener) *Service {
	return &Service{runner: commandRunner{}, open: open, now: time.Now}
}

func (service *Service) Create(ctx context.Context, databaseURL, storageRoot, destination string, includeFiles bool) error {
	if databaseURL == "" || storageRoot == "" || destination == "" {
		return ErrInvalidArchive
	}
	temporary, err := os.MkdirTemp("", "nexdrop-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	dumpPath := filepath.Join(temporary, "database.dump")
	if err := service.runner.Run(ctx, "pg_dump", "--format=custom", "--no-owner", "--file="+dumpPath, "--dbname="+databaseURL); err != nil {
		return fmt.Errorf("create PostgreSQL dump: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	succeeded := false
	defer func() {
		_ = file.Close()
		if !succeeded {
			_ = os.Remove(destination)
		}
	}()
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	manifest, err := json.Marshal(Manifest{FormatVersion: FormatVersion, CreatedAt: service.now().UTC(), IncludeFiles: includeFiles})
	if err == nil {
		err = writeBytes(tarWriter, "manifest.json", manifest, 0o600)
	}
	if err == nil {
		err = writeFile(tarWriter, dumpPath, "database.dump")
	}
	if err == nil && includeFiles {
		err = writeStorage(tarWriter, storageRoot)
	}
	if closeErr := tarWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gzipWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	succeeded = true
	return nil
}

func (service *Service) Restore(ctx context.Context, databaseURL, storageRoot, archivePath string) error {
	if databaseURL == "" || storageRoot == "" || archivePath == "" {
		return ErrInvalidArchive
	}
	revoked, err := service.currentRevocations(ctx, databaseURL)
	if err != nil {
		return err
	}
	temporary, err := os.MkdirTemp("", "nexdrop-restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	manifest, err := extractArchive(archivePath, temporary)
	if err != nil {
		return err
	}
	if manifest.FormatVersion != FormatVersion {
		return ErrInvalidArchive
	}
	restoreErr := service.runner.Run(ctx, "pg_restore", "--clean", "--if-exists", "--no-owner", "--dbname="+databaseURL, filepath.Join(temporary, "database.dump"))
	store, err := service.open(ctx, databaseURL)
	if err == nil {
		defer store.Close()
		err = store.ProtectRestoredSecurity(ctx, revoked, service.now().UTC())
	}
	if restoreErr != nil {
		return errors.Join(fmt.Errorf("restore PostgreSQL dump: %w", restoreErr), err)
	}
	if err != nil {
		return err
	}
	if manifest.IncludeFiles {
		return restoreFiles(filepath.Join(temporary, "files"), storageRoot)
	}
	return nil
}

func (service *Service) currentRevocations(ctx context.Context, databaseURL string) ([]string, error) {
	store, err := service.open(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.RevokedDeviceIDs(ctx)
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, name string, arguments ...string) error {
	command := exec.CommandContext(ctx, name, arguments...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command.Run()
}

func writeBytes(writer *tar.Writer, name string, content []byte, mode int64) error {
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(content)), ModTime: time.Unix(0, 0)}); err != nil {
		return err
	}
	_, err := writer.Write(content)
	return err
}

func writeFile(writer *tar.Writer, source, name string) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
		return err
	}
	_, err = io.Copy(writer, file)
	return err
}

func writeStorage(writer *tar.Writer, root string) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(absolute, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(absolute, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.IsDir() && (relative == "backups" || strings.HasPrefix(relative, "backups"+string(filepath.Separator))) {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		return writeFile(writer, path, filepath.ToSlash(filepath.Join("files", relative)))
	})
}

func extractArchive(archivePath, destination string) (Manifest, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return Manifest{}, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return Manifest{}, ErrInvalidArchive
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var manifest Manifest
	foundManifest, foundDump := false, false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Manifest{}, ErrInvalidArchive
		}
		name := filepath.ToSlash(filepath.Clean(header.Name))
		if name == "manifest.json" {
			if err := json.NewDecoder(io.LimitReader(reader, 1<<20)).Decode(&manifest); err != nil {
				return Manifest{}, ErrInvalidArchive
			}
			foundManifest = true
			continue
		}
		if name != "database.dump" && !strings.HasPrefix(name, "files/") {
			return Manifest{}, ErrInvalidArchive
		}
		target := filepath.Join(destination, filepath.FromSlash(name))
		if !within(destination, target) || header.Typeflag != tar.TypeReg {
			return Manifest{}, ErrInvalidArchive
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return Manifest{}, err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return Manifest{}, err
		}
		_, copyErr := io.Copy(output, reader)
		closeErr := output.Close()
		if copyErr != nil {
			return Manifest{}, copyErr
		}
		if closeErr != nil {
			return Manifest{}, closeErr
		}
		if name == "database.dump" {
			foundDump = true
		}
	}
	if !foundManifest || !foundDump {
		return Manifest{}, ErrInvalidArchive
	}
	return manifest, nil
}

func restoreFiles(source, destination string) error {
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if !within(destination, target) {
			return ErrInvalidArchive
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputClose, outputClose := input.Close(), output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputClose != nil {
			return inputClose
		}
		return outputClose
	})
}

func within(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
