//go:build linux

package monitoring

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemSamplerReadsLinuxResources(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "cached-file"), []byte("cache"), 0o600); err != nil {
		t.Fatal(err)
	}
	sampler := NewSystemSampler()
	result, err := sampler.Sample(root)
	if err != nil {
		t.Fatal(err)
	}
	if result.MemoryBytes <= 0 || result.DiskBytes <= 0 || result.CacheBytes != 5 {
		t.Fatalf("sample = %+v", result)
	}
}
