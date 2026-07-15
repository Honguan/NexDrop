package lan

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"net/http"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

type memoryChunkStore struct {
	mu     sync.Mutex
	chunks map[int][]byte
}

func (store *memoryChunkStore) CompletedChunks(context.Context, string, string) ([]int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]int, 0, len(store.chunks))
	for index := range store.chunks {
		result = append(result, index)
	}
	sort.Ints(result)
	return result, nil
}

func (store *memoryChunkStore) PutChunk(_ context.Context, _, _ string, index int, data []byte, expected [sha256.Size]byte) error {
	if sha256.Sum256(data) != expected {
		return errors.New("hash mismatch")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.chunks[index] = append([]byte(nil), data...)
	return nil
}

func (store *memoryChunkStore) CompleteFile(_ context.Context, _, _ string, chunkCount int, expected [sha256.Size]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.chunks) != chunkCount {
		return errors.New("incomplete")
	}
	hash := sha256.New()
	for index := 0; index < chunkCount; index++ {
		if _, ok := store.chunks[index]; !ok {
			return errors.New("incomplete")
		}
		_, _ = hash.Write(store.chunks[index])
	}
	returned := hash.Sum(nil)
	if !reflect.DeepEqual(returned, expected[:]) {
		return errors.New("file hash mismatch")
	}
	return nil
}

func TestTLSChunkTransferSupportsResumeAndMutualTrust(t *testing.T) {
	now := time.Now()
	sender, err := GenerateIdentity("sender01", now)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := GenerateIdentity("receive1", now)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryChunkStore{chunks: make(map[int][]byte)}
	server, err := NewTransferServer(receiver, StaticTrust{"sender01": sender.Fingerprint}, func() string { return "challenge-token" }, store)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && !errors.Is(serveErr, net.ErrClosed) {
			t.Errorf("Serve() error = %v", serveErr)
		}
	}()
	target := Advertisement{ShortDeviceID: "receive1", ServiceVersion: "1", Protocol: ProtocolVersion, Address: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Challenge: "challenge-token"}
	// Discovery challenges are normally 16-byte base64 values; use a valid one for target validation.
	target.Challenge = "Y2hhbGxlbmdlLXRva2VuIQ"
	challenge := target.Challenge
	server.challenge = func() string { return challenge }
	client, err := NewTransferClient(sender, StaticTrust{"receive1": receiver.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	first, second := []byte("encrypted chunk one"), []byte("encrypted chunk two")
	if err := client.PutChunk(context.Background(), target, "transfer01", "file0001", 0, first); err != nil {
		t.Fatal(err)
	}
	completed, err := client.CompletedChunks(context.Background(), target, "transfer01", "file0001")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(completed, []int{0}) {
		t.Fatalf("completed chunks = %v", completed)
	}
	if err := client.PutChunk(context.Background(), target, "transfer01", "file0001", 1, second); err != nil {
		t.Fatal(err)
	}
	whole := sha256.Sum256(append(append([]byte(nil), first...), second...))
	if err := client.Complete(context.Background(), target, "transfer01", "file0001", 2, whole); err != nil {
		t.Fatal(err)
	}
}

func TestTLSChunkTransferRejectsStaleChallenge(t *testing.T) {
	now := time.Now()
	sender, _ := GenerateIdentity("sender01", now)
	receiver, _ := GenerateIdentity("receive1", now)
	server, _ := NewTransferServer(receiver, StaticTrust{"sender01": sender.Fingerprint}, func() string { return "current-challenge" }, &memoryChunkStore{chunks: make(map[int][]byte)})
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go server.Serve(listener)
	target := Advertisement{ShortDeviceID: "receive1", ServiceVersion: "1", Protocol: ProtocolVersion, Address: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, Challenge: "c3RhbGUtY2hhbGxlbmdlIQ"}
	client, _ := NewTransferClient(sender, StaticTrust{"receive1": receiver.Fingerprint})
	if err := client.PutChunk(context.Background(), target, "transfer01", "file0001", 0, []byte("content")); err == nil {
		t.Fatal("stale discovery challenge was accepted")
	}
}
