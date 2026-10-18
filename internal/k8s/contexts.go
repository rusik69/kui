package k8s

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"sync"

	"k8s.io/client-go/tools/clientcmd"
)

// ContextInfo describes one entry from the kubeconfig.
type ContextInfo struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	Current   bool   `json:"current"`
}

// Manager loads kubeconfig and hands out cached per-context clients.
type Manager struct {
	loadingRules      *clientcmd.ClientConfigLoadingRules
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

// refreshIfKubeconfigChanged drops clients built from stale cluster, user, or
// context data. Context queries still load the source file on each call.
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
	fingerprint := hex.EncodeToString(hash.Sum(nil))
	m.mu.Lock()
	if fingerprint != m.configFingerprint {
		m.clients = make(map[string]*Clients)
		m.configFingerprint = fingerprint
	}
	m.mu.Unlock()
}

// ListContexts returns every context defined in the kubeconfig, sorted by name.
func (m *Manager) ListContexts() ([]ContextInfo, error) {
	m.refreshIfKubeconfigChanged()
	raw, err := m.loadingRules.Load()
	if err != nil {
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
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// DefaultContext returns the kubeconfig's current-context name.
func (m *Manager) DefaultContext() (string, error) {
	m.refreshIfKubeconfigChanged()
	raw, err := m.loadingRules.Load()
	if err != nil {
		return "", err
	}
	return raw.CurrentContext, nil
}
