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
	if set.Version != 0 || set.Rancher != nil || len(set.Ranchers) != 0 {
		t.Fatalf("expected zero-value settings, got %+v", set)
	}
	if got := store.Ranchers(); len(got) != 0 {
		t.Fatalf("Ranchers() on a missing file = %+v, want empty", got)
	}
}

func TestSaveRancherRoundTrip(t *testing.T) {
	store := newStore(t)
	want := &Rancher{URL: "https://rancher.example.com", Token: "tok-123", InsecureSkipTLSVerify: true}
	if err := store.SaveRancher(want); err != nil {
		t.Fatalf("SaveRancher: %v", err)
	}
	got := store.Ranchers()
	if len(got) != 1 {
		t.Fatalf("Ranchers() after save = %+v, want one connection", got)
	}
	if got[0].URL != want.URL || got[0].Token != want.Token || got[0].InsecureSkipTLSVerify != want.InsecureSkipTLSVerify {
		t.Fatalf("round trip = %+v, want %+v", got[0], want)
	}
	set, err := store.Load()
	if err != nil || set.Version != Version {
		t.Fatalf("version = %d, err = %v; want %d, nil", set.Version, err, Version)
	}
}

func TestSaveUpdatesSameURLAndAppendsNewServers(t *testing.T) {
	store := newStore(t)
	if err := store.SaveRancher(&Rancher{URL: "https://old.example.com", Token: "old"}); err != nil {
		t.Fatal(err)
	}
	// Same URL (any case): update in place, keeping the position.
	if err := store.SaveRancher(&Rancher{URL: "https://OLD.example.com", Token: "updated"}); err != nil {
		t.Fatal(err)
	}
	// A different server: appended, not replacing.
	if err := store.SaveRancher(&Rancher{URL: "https://new.example.com", Token: "new"}); err != nil {
		t.Fatal(err)
	}
	got := store.Ranchers()
	if len(got) != 2 {
		t.Fatalf("Ranchers() = %+v, want two connections", got)
	}
	if got[0].Token != "updated" {
		t.Fatalf("same-URL save did not update in place: %+v", got[0])
	}
	if got[1].Token != "new" {
		t.Fatalf("new server was not appended: %+v", got[1])
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

func TestClearRancherRemovesOnlyTheNamedServer(t *testing.T) {
	store := newStore(t)
	if err := store.SaveRancher(&Rancher{URL: "https://one.example.com", Token: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRancher(&Rancher{URL: "https://two.example.com", Token: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearRancher("https://ONE.example.com"); err != nil {
		t.Fatalf("ClearRancher: %v", err)
	}
	got := store.Ranchers()
	if len(got) != 1 || got[0].URL != "https://two.example.com" {
		t.Fatalf("after clear = %+v, want only two.example.com", got)
	}
	// Clearing an unknown URL is a no-op.
	if err := store.ClearRancher("https://unknown.example.com"); err != nil {
		t.Fatalf("ClearRancher unknown: %v", err)
	}
	if len(store.Ranchers()) != 1 {
		t.Fatalf("unknown URL clear changed the list: %+v", store.Ranchers())
	}
}

func TestClearRancherEmptyURLRemovesAll(t *testing.T) {
	store := newStore(t)
	for _, url := range []string{"https://one.example.com", "https://two.example.com"} {
		if err := store.SaveRancher(&Rancher{URL: url}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ClearRancher(""); err != nil {
		t.Fatalf("ClearRancher(all): %v", err)
	}
	if got := store.Ranchers(); len(got) != 0 {
		t.Fatalf("Ranchers survived disconnect-all: %+v", got)
	}
	// Clearing again on an empty file is a no-op.
	if err := store.ClearRancher(""); err != nil {
		t.Fatalf("second ClearRancher: %v", err)
	}
}

func TestVersionOneSettingsMigrateToTheList(t *testing.T) {
	store := newStore(t)
	if err := os.MkdirAll(store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A version-1 file: single connection under "rancher".
	legacy := `{"version":2,"rancher":{"url":"https://legacy.example.com","token":"legacy-token"}}`
	// Written with version 1 to simulate an old file.
	legacy = strings.Replace(legacy, `"version":2`, `"version":1`, 1)
	if err := os.WriteFile(store.Path(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	got := store.Ranchers()
	if len(got) != 1 || got[0].URL != "https://legacy.example.com" || got[0].Token != "legacy-token" {
		t.Fatalf("migration = %+v, want the legacy connection in the list", got)
	}
	// The migrated view must not resurrect the legacy field on save.
	if err := store.SaveRancher(&Rancher{URL: "https://two.example.com", Token: "t2"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	// The legacy token lives on inside the migrated list entry; what must be
	// gone is the legacy singular field (note: not the "ranchers" list).
	if strings.Contains(string(raw), `"rancher":{`) {
		t.Fatalf("save kept the legacy singular field: %s", raw)
	}
	got = store.Ranchers()
	if len(got) != 2 || got[0].Token != "legacy-token" || got[1].Token != "t2" {
		t.Fatalf("after migrate+save = %+v", got)
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
	if got := store.Ranchers(); len(got) != 0 {
		t.Fatalf("Ranchers() must be empty for a corrupt file, got %+v", got)
	}
	// Saving replaces the corrupt file.
	if err := store.SaveRancher(&Rancher{URL: "https://rancher.example.com", Token: "t"}); err != nil {
		t.Fatalf("SaveRancher over a corrupt file: %v", err)
	}
	if got := store.Ranchers(); len(got) != 1 || got[0].Token != "t" {
		t.Fatalf("Ranchers() after recovery save = %+v", got)
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
