package folder

import (
	"errors"
	"testing"
)

const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestValidateNestedFilesAndEmptyDirectories(t *testing.T) {
	manifest, err := Validate(Manifest{Version: 1, RootName: "Photos", Entries: []Entry{
		{Path: "2026", Type: EntryDirectory},
		{Path: "2026/empty", Type: EntryDirectory},
		{Path: "2026/相片.jpg", Type: EntryFile, Size: 10, SHA256: digest, FileID: "file-1"},
	}}, Limits{})
	if err != nil || len(manifest.Entries) != 3 {
		t.Fatalf("unexpected manifest: %#v %v", manifest, err)
	}
}

func TestRejectsTraversalAbsoluteDriveAndReservedNames(t *testing.T) {
	paths := []string{"../secret", "/etc/passwd", "C:\\secret", "folder/CON.txt"}
	for _, value := range paths {
		_, err := Validate(Manifest{Version: 1, RootName: "Root", Entries: []Entry{{Path: value, Type: EntryFile, SHA256: digest, FileID: "f"}}}, Limits{})
		if err == nil {
			t.Fatalf("expected path rejection for %q", value)
		}
	}
}

func TestRejectsCaseInsensitiveCollision(t *testing.T) {
	_, err := Validate(Manifest{Version: 1, RootName: "Root", Entries: []Entry{
		{Path: "A/file.txt", Type: EntryFile, SHA256: digest, FileID: "1"},
		{Path: "a/FILE.txt", Type: EntryFile, SHA256: digest, FileID: "2"},
	}}, Limits{})
	if !errors.Is(err, ErrPathCollision) {
		t.Fatalf("expected collision, got %v", err)
	}
}

func TestSelectIncludesRequiredParentDirectories(t *testing.T) {
	manifest := Manifest{Version: 1, RootName: "Root", Entries: []Entry{
		{Path: "a", Type: EntryDirectory},
		{Path: "a/b", Type: EntryDirectory},
		{Path: "a/b/file.txt", Type: EntryFile, SHA256: digest, FileID: "1"},
		{Path: "other.txt", Type: EntryFile, SHA256: digest, FileID: "2"},
	}}
	selected := Select(manifest, []string{"a/b/file.txt"})
	if len(selected.Entries) != 3 {
		t.Fatalf("unexpected selection: %#v", selected.Entries)
	}
}

func TestDuplicateContentSkipsSafelyBySizeAndHash(t *testing.T) {
	entry := Entry{Type: EntryFile, Size: 10, SHA256: digest}
	if action := ResolveConflict(10, digest, entry, ConflictOverwrite); action != ConflictSkip {
		t.Fatalf("expected safe duplicate skip, got %s", action)
	}
}
