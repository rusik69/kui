package k8s

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"
	"sync"

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
// optional Rancher source adds imported cluster contexts on top of the
// kubeconfig file without modifying it. The source can be attached, replaced,
// or removed at runtime (see SetRancher).
type Manager struct {
	loadingRules      *clientcmd.ClientConfigLoadingRules
	sourceMu          sync.RWMutex
	rancher           *RancherSource
	mu                sync.RWMutex
	clients           map[string]*Clients
	configFingerprint string
}

// NewManager builds a Manager from the given kubeconfig path (empty string
// means use the default loading rules / KUBECONFIG env var).
func NewManager(kubeconfigPath string) *Manager {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}
	return &Manager{
		loadingRules: rules,
		clients:      make(map[string]*Clients),
	}
}

// SetRancher attaches a Rancher source whose clusters appear as extra
// contexts. Passing nil removes the current source. Safe to call while the
// server is running; clients built for no-longer-imported clusters are
// dropped on the next fingerprint check.
func (m *Manager) SetRancher(source *RancherSource) {
	m.sourceMu.Lock()
	m.rancher = source
	m.sourceMu.Unlock()
	m.refreshIfKubeconfigChanged()
}

// Rancher returns the currently attached Rancher source, or nil.
func (m *Manager) Rancher() *RancherSource {
	m.sourceMu.RLock()
	defer m.sourceMu.RUnlock()
	return m.rancher
}

// refreshIfKubeconfigChanged drops clients built from stale cluster, user,
// or context data — including stale Rancher imports. Context queries still
// load the source file on each call.
func (m *Manager) refreshIfKubeconfigChanged() {
	hash := sha256.New()
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
	if m.Rancher() != nil {
		_, _ = hash.Write([]byte("rancher"))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(m.Rancher().Fingerprint()))
		_, _ = hash.Write([]byte{0})
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
// cluster imported from Rancher, sorted by name.
func (m *Manager) ListContexts() ([]ContextInfo, error) {
	m.refreshIfKubeconfigChanged()
	raw, err := m.loadFileConfig()
	if err != nil {
		if m.Rancher() == nil {
			return nil, err
		}
		// The kubeconfig file is unreadable; Rancher clusters may still be
		// usable on their own.
		if rancherContexts, rerr := m.Rancher().Contexts(context.Background()); rerr == nil {
			return rancherContexts, nil
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
	if m.Rancher() != nil {
		if rancherContexts, rerr := m.Rancher().Contexts(context.Background()); rerr == nil {
			out = append(out, rancherContexts...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// loadFileConfig loads the kubeconfig file without Rancher imports.
func (m *Manager) loadFileConfig() (*clientcmdapi.Config, error) {
	return m.loadingRules.Load()
}

// hasRancherContext reports whether ctxName names a Rancher-imported context.
func (m *Manager) hasRancherContext(ctxName string) bool {
	return m.Rancher() != nil && strings.HasPrefix(ctxName, RancherContextPrefix)
}

// DefaultContext returns the kubeconfig's current-context name. When the
// kubeconfig file is unreadable but Rancher is configured, it reports no
// current context instead of failing, so Rancher clusters remain usable.
func (m *Manager) DefaultContext() (string, error) {
	m.refreshIfKubeconfigChanged()
	raw, err := m.loadFileConfig()
	if err != nil {
		if m.Rancher() == nil {
			return "", err
		}
		return "", nil
	}
	return raw.CurrentContext, nil
}
