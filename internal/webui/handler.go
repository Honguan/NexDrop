package webui

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Handler struct {
	root      string
	indexPath string
}

func NewHandler(root string) (*Handler, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	indexPath := filepath.Join(absolute, "index.html")
	if info, err := os.Stat(indexPath); err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = errors.New("web index is not a regular file")
		}
		return nil, err
	}
	return &Handler{root: absolute, indexPath: indexPath}, nil
}

func (handler *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	requested := filepath.Join(handler.root, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
	if handler.withinRoot(requested) {
		if info, err := os.Stat(requested); err == nil && info.Mode().IsRegular() {
			if strings.HasPrefix(filepath.ToSlash(strings.TrimPrefix(requested, handler.root)), "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			http.ServeFile(w, r, requested)
			return
		}
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, handler.indexPath)
}

func (handler *Handler) withinRoot(path string) bool {
	relative, err := filepath.Rel(handler.root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
