// Package settings persists kui's own configuration — currently the Rancher
// management-server connection — under the user's config directory. The file
// is JSON, written atomically, and holds a bearer token, so it is created and
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
const Version = 1

const fileName = "settings.json"

// EnvToken and EnvURL are the environment variables consulted at startup.
const (
	EnvToken = "KUI_RANCHER_TOKEN"
	EnvURL   = "KUI_RANCHER_URL"
)

// Rancher is one persisted Rancher management-server connection.
type Rancher struct {
	URL                   string `json:"url"`
	Token                 string `json:"token,omitempty"`
	InsecureSkipTLSVerify bool   `json:"insecureSkipTLSVerify,omitempty"`
}

// Settings is the whole settings file.
type Settings struct {
	Version int      `json:"version"`
	Rancher *Rancher `json:"rancher,omitempty"`
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

// Load reads the settings file. A missing file is an empty Settings with a
// nil error; corrupt or unsupported files are reported as errors and callers
// should treat them as empty.
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
	if out.Version != Version {
		return Settings{}, fmt.Errorf("settings file %s has unsupported version %d (want %d)", s.Path(), out.Version, Version)
	}
	return out, nil
}

// Rancher returns the persisted Rancher connection, or nil.
func (s *Store) Rancher() *Rancher {
	set, err := s.Load()
	if err != nil {
		return nil
	}
	return set.Rancher
}

// SaveRancher persists one Rancher connection, replacing any previous one.
func (s *Store) SaveRancher(r *Rancher) error {
	set, err := s.Load()
	if err != nil {
		// A corrupt file is replaced rather than blocking the save; the
		// user explicitly asked to store new settings.
		set = Settings{}
	}
	set.Rancher = r
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

// Save writes settings atomically (temp file + rename) with 0600 permissions.
func (s *Store) Save(set Settings) error {
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

// ClearRancher removes the persisted Rancher connection, if any.
func (s *Store) ClearRancher() error {
	set, err := s.Load()
	if err != nil {
		set = Settings{}
	}
	if set.Rancher == nil {
		return nil
	}
	set.Rancher = nil
	return s.Save(set)
}
