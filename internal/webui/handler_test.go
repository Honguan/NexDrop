package webui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandlerServesAssetsAndSPAFallback(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>NexDrop</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "app.js"), []byte("app"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(root)
	if err != nil {
		t.Fatal(err)
	}

	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if asset.Code != http.StatusOK || asset.Body.String() != "app" || !strings.Contains(asset.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset response = %d, %q, %q", asset.Code, asset.Body.String(), asset.Header().Get("Cache-Control"))
	}
	fallback := httptest.NewRecorder()
	handler.ServeHTTP(fallback, httptest.NewRequest(http.MethodGet, "/activity", nil))
	content, _ := io.ReadAll(fallback.Result().Body)
	if fallback.Code != http.StatusOK || !strings.Contains(string(content), "NexDrop") || fallback.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("fallback response = %d, %q, %q", fallback.Code, content, fallback.Header().Get("Cache-Control"))
	}
}

func TestNewHandlerRequiresIndex(t *testing.T) {
	if _, err := NewHandler(t.TempDir()); err == nil {
		t.Fatal("NewHandler() error = nil")
	}
}
