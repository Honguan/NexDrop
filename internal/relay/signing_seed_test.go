package relay

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRelaySigningSeedPersistsAcrossRestarts(t *testing.T) {
	root := t.TempDir()
	t.Setenv("NEXDROP_STORAGE_PATH", root)
	t.Setenv("NEXDROP_RELAY_SIGNING_SEED", "replace-with-openssl-rand-hex-32")

	first, err := NewGrantSigner([]byte("fallback-seed-that-must-not-be-used-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewGrantSigner([]byte("fallback-seed-that-must-not-be-used-2"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.PublicKey(), second.PublicKey()) {
		t.Fatal("relay signing identity changed across restarts")
	}
	info, err := os.Stat(filepath.Join(root, ".relay-signing-seed"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected seed permissions: %o", info.Mode().Perm())
	}
}

func TestConfiguredRelaySigningSeedsAreIndependent(t *testing.T) {
	t.Setenv("NEXDROP_STORAGE_PATH", "")
	t.Setenv("NEXDROP_RELAY_SIGNING_SEED", "relay-signing-seed-node-a-32-bytes")
	first, err := NewGrantSigner(nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEXDROP_RELAY_SIGNING_SEED", "relay-signing-seed-node-b-32-bytes")
	second, err := NewGrantSigner(nil)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.PublicKey(), second.PublicKey()) {
		t.Fatal("different Nodes must not share relay signing identities")
	}
}
