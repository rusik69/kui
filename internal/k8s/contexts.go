package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ContextInfo describes one entry from the kubeconfig.
type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Current   bool   `json:"current"`
}

// Manager loads kubeconfig and hands out cached per-context clients. An
// optional set of Rancher sources adds imported cluster contexts on top of
// the kubeconfig file without modifying it. Sources can be attached,
// replaced, or removed at runtime (see SetRancherSources).
type Manager struct {
	loadingRules      *clientcmd.ClientConfigLoadingRules
	kubeconfigPath    string // as given: a file, a directory, or ""
	sourceMu          sync.RWMutex
	ranchers          []*RancherSource
	mu                sync.RWMutex
	clients           map[string]*Clients
	configFingerprint string
}

// NewManager builds a Manager from the given kubeconfig path (empty string
// means use the default loading rules / KUBECONFIG env var). If the path is
// a directory, all kubeconfig files in it (*.yaml, *.yml, *.json, ignoring
// hidden files) are loaded and merged; the directory is re-scanned on every
// context query so files can be added, edited, or removed while kui runs.
func NewManager(kubeconfigPath string) *Manager {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		applyKubeconfigPath(rules, kubeconfigPath)
	}
	return &Manager{
		loadingRules:   rules,
		kubeconfigPath: kubeconfigPath,
		clients:        make(map[string]*Clients),
	}
}

// applyKubeconfigPath configures rules for a file or directory path.
func applyKubeconfigPath(rules *clientcmd.ClientConfigLoadingRules, path string) {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		rules.Precedence = kubeconfigPaths(path)
		rules.ExplicitPath = ""
	} else {
		rules.ExplicitPath = path
	}
}

// SetKubeconfigDir switches the kubeconfig source to a directory at runtime
// ("" reverts to the default loading rules / KUBECONFIG env var). Clients
// built against the previous source are dropped on the next fingerprint
// check. Returns an error when dir exists but is not a directory.
func (m *Manager) SetKubeconfigDir(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir != "" {
		if fi, err := os.Stat(dir); err != nil {
			return fmt.Errorf("kubeconfig directory %q: %w", dir, err)
		} else if !fi.IsDir() {
			return fmt.Errorf("kubeconfig path %q is not a directory", dir)
		}
	}
	m.mu.Lock()
	m.loadingRules = clientcmd.NewDefaultClientConfigLoadingRules()
	if dir != "" {
		applyKubeconfigPath(m.loadingRules, dir)
	}
	m.kubeconfigPath = dir
	m.clients = make(map[string]*Clients)
	m.configFingerprint = ""
	m.mu.Unlock()
	return nil
}

// KubeconfigDir returns the active kubeconfig source: a directory path, a
// file path, or "" for the default loading rules.
func (m *Manager) KubeconfigDir() string {
	return m.kubeconfigPath
}

// kubeconfigPaths returns the sorted list of kubeconfig files inside dir
// (*.yaml, *.yml, *.json, ignoring hidden and non-regular files). Sorted so
// merge precedence is deterministic (client-go: first file wins on map-key
// conflicts).
func kubeconfigPaths(dir string) []string {
	var paths []string
	for _, pattern := range []string{"*.yaml", "*.yml", "*.json"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	out := paths[:0]
	for _, p := range paths {
		if strings.HasPrefix(filepath.Base(p), ".") {
			continue
		}
		if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
			continue
		}
		out = append(out, p)
	}
	return out
}

// SetRancher attaches a single Rancher source, replacing every previously
// attached one (nil removes all). Kept for the single-server flag path and
// existing callers; multi-server code uses SetRancherSources.
func (m *Manager) SetRancher(source *RancherSource) {
	var sources []*RancherSource
	if source != nil {
		sources = []*RancherSource{source}
	}
	m.SetRancherSources(sources)
}

// SetRancherSources attaches several Rancher sources at once, replacing
// every previously attached one. Passing nil or an empty list removes all.
// Safe to call while the server is running; clients built for
// no-longer-imported clusters are dropped on the next fingerprint check.
// Context names are namespaced per server: with zero or one attached source
// clusters keep the plain "rancher/<name>" shape, with two or more the names
// gain the server ID ("rancher/<server>/<name>") so the same cluster name on
// two servers never collides.
func (m *Manager) SetRancherSources(sources []*RancherSource) {
	m.sourceMu.Lock()
	m.ranchers = sources
	m.sourceMu.Unlock()
	m.refreshIfKubeconfigChanged()
}

// Ranchers returns the currently attached Rancher sources.
func (m *Manager) Ranchers() []*RancherSource {
	m.sourceMu.RLock()
	defer m.sourceMu.RUnlock()
	return m.ranchers
}

// Rancher returns the single attached source when exactly one is connected,
// or nil (zero or several sources) — the single-server compatibility view.
func (m *Manager) Rancher() *RancherSource {
	m.sourceMu.RLock()
	defer m.sourceMu.RUnlock()
	if len(m.ranchers) == 1 {
		return m.ranchers[0]
	}
	return nil
}

// refreshIfKubeconfigChanged drops clients built from stale cluster, user,
// or context data — including stale Rancher imports. Context queries still
// load the source file(s) on each call.
func (m *Manager) refreshIfKubeconfigChanged() {
	hash := sha256.New()
	// Directory mode: re-expand the directory on every check so files added
	// or removed while kui runs are picked up (and invalidate cached
	// clients). File mode fingerprints the explicit path.
	if fi, err := os.Stat(m.kubeconfigPath); err == nil && fi.IsDir() {
		paths := kubeconfigPaths(m.kubeconfigPath)
		_, _ = hash.Write([]byte("dir:" + m.kubeconfigPath))
		_, _ = hash.Write([]byte{0})
		for _, path := range paths {
			_, _ = hash.Write([]byte(path))
			_, _ = hash.Write([]byte{0})
			content, err := os.ReadFile(path)
			if err != nil {
				_, _ = hash.Write([]byte(err.Error()))
			} else {
				_, _ = hash.Write(content)
			}
			_, _ = hash.Write([]byte{0})
		}
		m.loadingRules.Precedence = paths
	} else {
		for _, path := range m.loadingRules.GetLoadingPrecedence() {
			_, _ = hash.Write([]byte(path))
			_, _ = hash.Write([]byte{0})
			content, err := os.ReadFile(path)
			if err != nil {
				_, _ = hash.Write([]byte(err.Error()))
			} else {
				_, _ = hash.Write(content)
			}
			_, _ = hash.Write([]byte{0})
		}
	}
	if ranchers := m.Ranchers(); len(ranchers) > 0 {
		_, _ = hash.Write([]byte("rancher"))
		_, _ = hash.Write([]byte{0})
		for _, source := range ranchers {
			_, _ = hash.Write([]byte(source.ID()))
			_, _ = hash.Write([]byte{0})
			_, _ = hash.Write([]byte(source.Fingerprint()))
			_, _ = hash.Write([]byte{0})
		}
	}
	fingerprint := hex.EncodeToString(hash.Sum(nil))
	m.mu.Lock()
	if fingerprint != m.configFingerprint {
		m.clients = make(map[string]*Clients)
		m.configFingerprint = fingerprint
	}
	m.mu.Unlock()
}

// ListContexts returns every context defined in the kubeconfig plus every
// cluster imported from every connected Rancher server, sorted by name.
func (m *Manager) ListContexts() ([]ContextInfo, error) {
	m.refreshIfKubeconfigChanged()
	raw, err := m.loadFileConfig()
	ranchers := m.Ranchers()
	if err != nil {
		if len(ranchers) == 0 {
			return nil, err
		}
		// The kubeconfig file is unreadable; Rancher clusters may still be
		// usable on their own.
		out, rerr := m.rancherContexts(context.Background(), ranchers)
		if rerr == nil && len(out) > 0 {
			return out, nil
		}
		return nil, err
	}
	out := make([]ContextInfo, 0, len(raw.Contexts))
	for name, ctx := range raw.Contexts {
		out = append(out, ContextInfo{
			Name:      name,
			Cluster:   ctx.Cluster,
			Namespace: ctx.Namespace,
			Current:   name == raw.CurrentContext,
		})
	}
	if len(ranchers) > 0 {
		if rancherContexts, rerr := m.rancherContexts(context.Background(), ranchers); rerr == nil {
			out = append(out, rancherContexts...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// rancherContexts merges every source's imported clusters into one sorted
// view, namespacing context names per server once more than one server is
// connected (see SetRancherSources).
func (m *Manager) rancherContexts(ctx context.Context, sources []*RancherSource) ([]ContextInfo, error) {
	multi := len(sources) > 1
	out := make([]ContextInfo, 0, len(sources)*4)
	used := map[string]bool{}
	for _, source := range sources {
		contexts, err := source.Contexts(ctx)
		if err != nil {
			continue // one unreachable server must not hide the others
		}
		for _, info := range contexts {
			name := info.Name
			if multi {
				name = RancherContextPrefix + source.ID() + "/" + strings.TrimPrefix(name, RancherContextPrefix)
			}
			if used[name] {
				continue // defensive: identical names across sources are dropped
			}
			used[name] = true
			info.Name = name
			out = append(out, info)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// loadFileConfig loads the kubeconfig file(s) without Rancher imports.
func (m *Manager) loadFileConfig() (*clientcmdapi.Config, error) {
	return m.loadingRules.Load()
}

// hasRancherContext reports whether ctxName names a Rancher-imported context.
// With multiple servers the name carries the server ID
// ("rancher/<server>/<cluster>"); every "rancher/"-prefixed name is handled
// by rancherContextFor.
func (m *Manager) hasRancherContext(ctxName string) bool {
	return strings.HasPrefix(ctxName, RancherContextPrefix) && len(m.Ranchers()) > 0
}

// rancherContextFor resolves a Rancher-imported context name against the
// attached sources. Names are tried per source in attached order: with one
// server the plain "rancher/<cluster>" shape matches directly; with several
// servers the name embeds the server ID ("rancher/<server>/<cluster>") and
// only the matching source can own it.
func (m *Manager) rancherContextFor(ctx context.Context, ctxName string) (*rest.Config, error) {
	sources := m.Ranchers()
	for _, source := range sources {
		if !source.OwnsContext(ctxName) {
			continue
		}
		if cfg, err := source.RestConfig(ctx, ctxName); err == nil {
			return cfg, nil
		}
	}
	// Legacy shape ("rancher/<cluster>") with several servers attached: the
	// name came from a pre-upgrade session. Try every source; the first
	// holding this context name wins, and ambiguity is resolved by the sort
	// order of ListContexts (the same name from two servers collapses there).
	for _, source := range sources {
		if cfg, err := source.RestConfig(ctx, ctxName); err == nil {
			return cfg, nil
		}
	}
	return nil, fmt.Errorf("rancher context %q is not available on any connected server", ctxName)
}

// DefaultContext returns the kubeconfig's current-context name. When the
// kubeconfig file is unreadable but Rancher is configured, it reports no
// current context instead of failing, so Rancher clusters remain usable.
func (m *Manager) DefaultContext() (string, error) {
	m.refreshIfKubeconfigChanged()
	raw, err := m.loadFileConfig()
	if err != nil {
		if len(m.Ranchers()) == 0 {
			return "", err
		}
		return "", nil
	}
	return raw.CurrentContext, nil
}
