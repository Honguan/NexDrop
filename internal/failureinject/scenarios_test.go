package failureinject

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/filetransfer"
	"nexdrop/internal/maintenance"
)

func TestPacketLossAfterControlledLatency(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer connection.Close()
		time.Sleep(25 * time.Millisecond)
		_, writeErr := connection.Write([]byte("partial"))
		serverDone <- writeErr
	}()

	started := time.Now()
	connection, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	payload, readErr := io.ReadAll(connection)
	_ = connection.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("injected latency = %s, want at least 20ms", elapsed)
	}
	if string(payload) != "partial" {
		t.Fatalf("truncated payload = %q", payload)
	}
}

func TestReceiverPauseTerminationAndReconnect(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		sleeping, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		time.Sleep(40 * time.Millisecond)
		_ = sleeping.Close()
		awake, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		_, writeErr := awake.Write([]byte("awake"))
		_ = awake.Close()
		serverDone <- writeErr
	}()

	sleeping, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = sleeping.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
	buffer := make([]byte, 5)
	if _, err := sleeping.Read(buffer); err == nil {
		t.Fatal("paused receiver unexpectedly responded")
	} else {
		var networkError net.Error
		if !errors.As(err, &networkError) || !networkError.Timeout() {
			t.Fatalf("paused receiver error = %v, want timeout", err)
		}
	}
	_ = sleeping.Close()

	awake, err := net.DialTimeout("tcp4", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = awake.SetReadDeadline(time.Now().Add(time.Second))
	payload, err := io.ReadAll(awake)
	_ = awake.Close()
	if err != nil || string(payload) != "awake" {
		t.Fatalf("reconnected payload = %q, %v", payload, err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestAddressFamilyAsymmetryAndInterfaceChange(t *testing.T) {
	first, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	firstPort := first.Addr().(*net.TCPAddr).Port
	firstDone := serveOnce(first, "first")
	if connection, err := net.DialTimeout("tcp6", net.JoinHostPort("::1", strconv.Itoa(firstPort)), 30*time.Millisecond); err == nil {
		_ = connection.Close()
		t.Fatal("IPv6 unexpectedly reached IPv4-only listener")
	}
	if payload := dialPayload(t, first.Addr().String()); payload != "first" {
		t.Fatalf("IPv4 payload = %q", payload)
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	_ = first.Close()

	second, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	secondDone := serveOnce(second, "second")
	if connection, err := net.DialTimeout("tcp4", first.Addr().String(), 30*time.Millisecond); err == nil {
		_ = connection.Close()
		t.Fatal("retired interface address still accepted connections")
	}
	if payload := dialPayload(t, second.Addr().String()); payload != "second" {
		t.Fatalf("changed interface payload = %q", payload)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
}

func serveOnce(listener net.Listener, payload string) <-chan error {
	done := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err == nil {
			_, err = connection.Write([]byte(payload))
			_ = connection.Close()
		}
		done <- err
	}()
	return done
}

func dialPayload(t *testing.T, address string) string {
	t.Helper()
	connection, err := net.DialTimeout("tcp4", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	payload, err := io.ReadAll(connection)
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

type concurrentStore struct {
	mu      sync.Mutex
	file    filetransfer.FileRecord
	chunks  map[int]filetransfer.ChunkRecord
	expired maintenance.ExpiredFile
	marked  bool
}

func (store *concurrentStore) PrepareChunkUpload(_ context.Context, _ auth.Session, _ string, index int) (filetransfer.FileRecord, *filetransfer.ChunkRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if chunk, ok := store.chunks[index]; ok {
		copy := chunk
		return store.file, &copy, nil
	}
	return store.file, nil, nil
}

func (store *concurrentStore) RecordChunk(_ context.Context, _ auth.Session, chunk filetransfer.ChunkRecord) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, ok := store.chunks[chunk.Index]; ok && !bytes.Equal(existing.SHA256, chunk.SHA256) {
		return filetransfer.ErrConflict
	}
	store.chunks[chunk.Index] = chunk
	return nil
}

func (store *concurrentStore) OpenChunk(context.Context, auth.Session, string, int) (filetransfer.ChunkRecord, error) {
	return filetransfer.ChunkRecord{}, filetransfer.ErrNotFound
}

func (store *concurrentStore) PrepareFileCompletion(context.Context, auth.Session, string) (filetransfer.FileRecord, []filetransfer.ChunkRecord, error) {
	return store.file, nil, nil
}

func (store *concurrentStore) CompleteFile(context.Context, auth.Session, string, string, time.Time) error {
	return nil
}

func (store *concurrentStore) ExpiredFiles(context.Context, time.Time, int) ([]maintenance.ExpiredFile, error) {
	return []maintenance.ExpiredFile{store.expired}, nil
}

func (store *concurrentStore) MarkFileExpired(context.Context, string, time.Time) error {
	store.mu.Lock()
	store.marked = true
	store.mu.Unlock()
	return nil
}

func TestCleanupAndIdempotentReplayDuringActiveTransfer(t *testing.T) {
	root := t.TempDir()
	expiredPath := filepath.Join(root, "expired.bin")
	if err := os.WriteFile(expiredPath, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := []byte("data")
	digest := sha256.Sum256(content)
	store := &concurrentStore{
		file: filetransfer.FileRecord{
			ID: "active-file", Size: int64(len(content)), SHA256: digest[:],
			ChunkSize: len(content), ChunkCount: 1,
		},
		chunks:  make(map[int]filetransfer.ChunkRecord),
		expired: maintenance.ExpiredFile{ID: "expired-file", TransferID: "expired-transfer", StoragePath: expiredPath},
	}
	files, err := filetransfer.NewService(store, root)
	if err != nil {
		t.Fatal(err)
	}
	cleaner, err := maintenance.NewCleaner(store, root)
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errorsChannel := make(chan error, 2)
	group.Add(2)
	go func() {
		defer group.Done()
		_, uploadErr := files.UploadChunk(context.Background(), auth.Session{}, "active-file", 0, digest[:], bytes.NewReader(content))
		errorsChannel <- uploadErr
	}()
	go func() {
		defer group.Done()
		_, cleanupErr := cleaner.RunOnce(context.Background(), 10)
		errorsChannel <- cleanupErr
	}()
	group.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}

	replays := make(chan error, 4)
	for range 4 {
		go func() {
			_, replayErr := files.UploadChunk(context.Background(), auth.Session{}, "active-file", 0, digest[:], bytes.NewReader(content))
			replays <- replayErr
		}()
	}
	for range 4 {
		if err := <-replays; err != nil {
			t.Fatal(err)
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.chunks) != 1 || !store.marked {
		t.Fatalf("chunks = %d, expired marked = %v", len(store.chunks), store.marked)
	}
	if _, err := os.Stat(expiredPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired path remains: %v", err)
	}
}
