// Package settings persists kui's own configuration — currently the Rancher
// management-server connections — under the user's config directory. The file
// is JSON, written atomically, and holds bearer tokens, so it is created and
// kept at 0600 permissions like a kubeconfig.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Version is the on-disk schema version. Bump it when the shape changes
// incompatibly; older files are rejected rather than misread.
const Version = 2

const fileName = "settings.json"

// EnvToken and EnvURL are the environment variables consulted at startup.
const (
	EnvToken = "KUI_RANCHER_TOKEN"
	EnvURL   = "KUI_RANCHER_URL"
)

// Rancher is one persisted Rancher management-server connection. Name is a
// stable per-connection identifier (the server's hostname) used to namespace
// imported context names; it is derived on save, not user-chosen.
type Rancher struct {
	URL                   string `json:"url"`
	Token                 string `json:"token,omitempty"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify,omitempty"`
}

// Kubeconfig is a persisted kubeconfig source. Dir holds a directory of
// kubeconfig files ("" when unset); a single explicit file is configured via
// the --kubeconfig flag and is never stored here.
type Kubeconfig struct {
	Dir string `json:"dir,omitempty"`
}

// Settings is the whole settings file. Version 2 keeps the version-1 single
// connection under Rancher only as a load-time migration input: Save always
// writes the Ranchers list, and Load folds a legacy entry into it.
type Settings struct {
	Version int `json:"version"`
	// Rancher is the legacy version-1 single connection. Load migrates it
	// into Ranchers; new saves leave it nil.
	Rancher *Rancher `json:"rancher,omitempty"`
	// Ranchers holds every persisted Rancher connection. A single connection
	// is stored here too (len 1), so the list is the only runtime shape.
	Ranchers   []Rancher   `json:"ranchers,omitempty"`
	Kubeconfig *Kubeconfig `json:"kubeconfig,omitempty"`
}

// Load reads the settings file and normalizes it to the current shape: a
// legacy version-1 single connection is folded into Ranchers (and cleared
// from the legacy field in the returned view). The on-disk file is untouched
// until the next Save.
func (s *Store) Load() (Settings, error) {
	data, err := os.ReadFile(s.Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	var out Settings
	if err := json.Unmarshal(data, &out); err != nil {
		return Settings{}, fmt.Errorf("settings file %s is corrupt: %w", s.Path(), err)
	}
	// Version 1 files carry one connection under "rancher"; normalize folds
	// it into the list. Anything else unknown is rejected rather than
	// misread.
	if out.Version != 1 && out.Version != Version {
		return Settings{}, fmt.Errorf("settings file %s has unsupported version %d (want %d)", s.Path(), out.Version, Version)
	}
	return normalize(out), nil
}

// normalize folds the legacy single connection into the list. Used by Load
// and Save so every reader sees one shape.
func normalize(set Settings) Settings {
	if set.Rancher != nil {
		// Migrate unless the same URL is already in the list (a concurrent
		// path could have written both shapes).
		known := false
		for _, existing := range set.Ranchers {
			if strings.EqualFold(existing.URL, set.Rancher.URL) {
				known = true
				break
			}
		}
		if !known {
			set.Ranchers = append([]Rancher{*set.Rancher}, set.Ranchers...)
		}
		set.Rancher = nil
	}
	return set
}

// Store reads and writes settings.json inside one directory. An empty dir
// makes every operation fail cleanly (used when no config dir exists).
type Store struct {
	dir string
}

// DefaultStore returns the store for the user's config directory
// (~/Library/Application Support/kui, ~/.config/kui, %AppData%\kui).
// KUI_CONFIG_DIR overrides the location.
func DefaultStore() (*Store, error) {
	if dir := strings.TrimSpace(os.Getenv("KUI_CONFIG_DIR")); dir != "" {
		return &Store{dir: dir}, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{dir: filepath.Join(base, "kui")}, nil
}

// Path is the settings file location, for log and UI messages.
func (s *Store) Path() string {
	return filepath.Join(s.dir, fileName)
}

// Ranchers returns the persisted Rancher connections, oldest first (the
// migration prepends the legacy entry, so a migrated single connection stays
// first).
func (s *Store) Ranchers() []Rancher {
	set, err := s.Load()
	if err != nil {
		return nil
	}
	return set.Ranchers
}

// SaveRancher adds or updates one connection, keyed by URL: an existing
// entry with the same URL (case-insensitive) is replaced in place, a new one
// is appended. The connection's Name field is ignored here; it is derived
// where needed.
func (s *Store) SaveRancher(r *Rancher) error {
	set, err := s.Load()
	if err != nil {
		// A corrupt file is replaced rather than blocking the save; the
		// user explicitly asked to store new settings.
		set = Settings{}
	}
	set = normalize(set)
	replaced := false
	for i := range set.Ranchers {
		if strings.EqualFold(set.Ranchers[i].URL, r.URL) {
			set.Ranchers[i] = *r
			replaced = true
			break
		}
	}
	if !replaced {
		set.Ranchers = append(set.Ranchers, *r)
	}
	return s.Save(set)
}

// TokenFromFlagOrEnv resolves the Rancher token at startup: the CLI flag
// value wins, then the KUI_RANCHER_TOKEN environment variable.
func TokenFromFlagOrEnv(flagToken string) string {
	if strings.TrimSpace(flagToken) != "" {
		return strings.TrimSpace(flagToken)
	}
	return strings.TrimSpace(os.Getenv(EnvToken))
}

// DefaultStoreOrNoop returns the default store, or a store bound to a
// directory that cannot exist when the platform config dir is unavailable.
// Handlers treat a failing store as "save reports the error"; this never
// panics and never falls back to a shared temp location.
func DefaultStoreOrNoop() *Store {
	store, err := DefaultStore()
	if err != nil {
		return &Store{dir: ""}
	}
	return store
}

// Save writes settings atomically (temp file + rename) with 0600 permissions
// in the current schema: the legacy single-connection field is folded into
// the list first, so the file always round-trips through one shape.
func (s *Store) Save(set Settings) error {
	set = normalize(set)
	set.Version = Version
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := s.Path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.Path()); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// The rename target may predate this write; enforce the mode either way.
	_ = os.Chmod(s.Path(), 0o600)
	return nil
}

// ClearRancher removes one persisted connection by URL (case-insensitive).
// An empty URL removes every connection — the "disconnect all" path.
func (s *Store) ClearRancher(url string) error {
	set, err := s.Load()
	if err != nil {
		set = Settings{}
	}
	set = normalize(set)
	if len(set.Ranchers) == 0 {
		return nil
	}
	if strings.TrimSpace(url) == "" {
		set.Ranchers = nil
		return s.Save(set)
	}
	kept := set.Ranchers[:0]
	for _, existing := range set.Ranchers {
		if !strings.EqualFold(existing.URL, url) {
			kept = append(kept, existing)
		}
	}
	set.Ranchers = kept
	return s.Save(set)
}

// Kubeconfig returns the persisted kubeconfig settings, or nil.
func (s *Store) Kubeconfig() *Kubeconfig {
	set, err := s.Load()
	if err != nil {
		return nil
	}
	return set.Kubeconfig
}

// SaveKubeconfig persists the kubeconfig settings, replacing any previous one.
func (s *Store) SaveKubeconfig(k *Kubeconfig) error {
	set, err := s.Load()
	if err != nil {
		set = Settings{}
	}
	set = normalize(set)
	set.Kubeconfig = k
	return s.Save(set)
}
