package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{dir: filepath.Join(t.TempDir(), "config", "kui")}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	store := newStore(t)
	set, err := store.Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if set.Version != 0 || set.Rancher != nil {
		t.Fatalf("expected zero-value settings, got %+v", set)
	}
	if store.Rancher() != nil {
		t.Fatal("Rancher() on a missing file must be nil")
	}
}

func TestSaveRancherRoundTrip(t *testing.T) {
	store := newStore(t)
	want := &Rancher{URL: "https://rancher.example.com", Token: "tok-123", InsecureSkipTLSVerify: true}
	if err := store.SaveRancher(want); err != nil {
		t.Fatalf("SaveRancher: %v", err)
	}
	got := store.Rancher()
	if got == nil {
		t.Fatal("Rancher() returned nil after save")
	}
	if got.URL != want.URL || got.Token != want.Token || got.InsecureSkipTLSVerify != want.InsecureSkipTLSVerify {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	set, err := store.Load()
	if err != nil || set.Version != Version {
		t.Fatalf("version = %d, err = %v; want %d, nil", set.Version, err, Version)
	}
}

func TestSaveOverwritesPreviousConnection(t *testing.T) {
	store := newStore(t)
	if err := store.SaveRancher(&Rancher{URL: "https://old.example.com", Token: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRancher(&Rancher{URL: "https://new.example.com", Token: "new"}); err != nil {
		t.Fatal(err)
	}
	got := store.Rancher()
	if got.URL != "https://new.example.com" || got.Token != "new" {
		t.Fatalf("after overwrite = %+v", got)
	}
}

func TestFilePermissionsAreRestricted(t *testing.T) {
	store := newStore(t)
	if err := store.SaveRancher(&Rancher{URL: "https://rancher.example.com", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("settings file mode = %o, want 600", perm)
	}
	// The directory must be private too.
	dirInfo, err := os.Stat(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("settings dir mode = %o, want 700", perm)
	}
}

func TestClearRancher(t *testing.T) {
	store := newStore(t)
	if err := store.SaveRancher(&Rancher{URL: "https://rancher.example.com"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearRancher(); err != nil {
		t.Fatalf("ClearRancher: %v", err)
	}
	if store.Rancher() != nil {
		t.Fatal("Rancher survived ClearRancher")
	}
	// Clearing again on an empty file is a no-op.
	if err := store.ClearRancher(); err != nil {
		t.Fatalf("second ClearRancher: %v", err)
	}
}

func TestCorruptFileIsAnErrorNotSilence(t *testing.T) {
	store := newStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("Load accepted a corrupt file")
	}
	if store.Rancher() != nil {
		t.Fatal("Rancher() must be nil for a corrupt file")
	}
	// Saving replaces the corrupt file.
	if err := store.SaveRancher(&Rancher{URL: "https://rancher.example.com", Token: "t"}); err != nil {
		t.Fatalf("SaveRancher over a corrupt file: %v", err)
	}
	if got := store.Rancher(); got == nil || got.Token != "t" {
		t.Fatalf("Rancher() after recovery save = %+v", got)
	}
}

func TestWrongVersionIsRejected(t *testing.T) {
	store := newStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := store.Load()
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("Load error = %v, want unsupported version", err)
	}
}

func TestDefaultStoreRespectsEnvOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "custom")
	t.Setenv("KUI_CONFIG_DIR", dir)
	store, err := DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	if store.Path() != filepath.Join(dir, fileName) {
		t.Fatalf("path = %q, want under %q", store.Path(), dir)
	}
	if err := store.SaveRancher(&Rancher{URL: "https://x"}); err != nil {
		t.Fatal(err)
	}
}
