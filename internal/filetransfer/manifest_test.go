package filetransfer

import (
	"bytes"
	"errors"
	"testing"
)

func TestBuildManifest(t *testing.T) {
	manifest, err := BuildManifest(bytes.NewBufferString("abcdefghij"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Size != 10 {
		t.Fatalf("Size = %d, want 10", manifest.Size)
	}
	if len(manifest.Chunks) != 3 {
		t.Fatalf("len(Chunks) = %d, want 3", len(manifest.Chunks))
	}
	if manifest.Chunks[2].Size != 2 {
		t.Fatalf("last chunk size = %d, want 2", manifest.Chunks[2].Size)
	}
	if !manifest.VerifyChunk(1, []byte("efgh")) {
		t.Fatal("valid chunk was rejected")
	}
}

func TestManifestRejectsModifiedChunk(t *testing.T) {
	manifest, err := BuildManifest(bytes.NewBufferString("abcdefgh"), 4)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.VerifyChunk(1, []byte("EFGH")) {
		t.Fatal("modified chunk was accepted")
	}
	if manifest.VerifyChunk(2, nil) {
		t.Fatal("out-of-range chunk was accepted")
	}
}

func TestManifestVerifyFile(t *testing.T) {
	manifest, err := BuildManifest(bytes.NewBufferString("complete file"), 5)
	if err != nil {
		t.Fatal(err)
	}

	valid, err := manifest.VerifyFile(bytes.NewBufferString("complete file"))
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("valid file was rejected")
	}

	valid, err = manifest.VerifyFile(bytes.NewBufferString("changed file"))
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("changed file was accepted")
	}
}

func TestBuildManifestRejectsInvalidChunkSize(t *testing.T) {
	_, err := BuildManifest(bytes.NewBuffer(nil), 0)
	if !errors.Is(err, ErrInvalidChunkSize) {
		t.Fatalf("BuildManifest() error = %v, want ErrInvalidChunkSize", err)
	}
}
