package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// SnapshotKubeconfig writes a self-contained kubeconfig for one context to a
// 0600 file and returns its path. The local kubectl terminal uses it so
// commands run against exactly the context kui has selected — including
// Rancher-imported contexts, whose credentials exist only in memory.
//
// File-backed contexts are materialized from the loaded kubeconfig (with the
// requested context set as current-context); Rancher contexts from the
// source's merged in-memory view. The snapshot is a copy: kui never edits
// the user's kubeconfig, and each terminal session gets its own file under
// the session temp dir so concurrent sessions cannot race.
func (m *Manager) SnapshotKubeconfig(ctxName string) (string, error) {
	if hasR := m.hasRancherContext(ctxName); hasR {
		for _, source := range m.Ranchers() {
			if !source.OwnsContext(ctxName) {
				continue
			}
			cfg, err := source.mergedConfig(context.Background())
			if err != nil {
				return "", fmt.Errorf("build Rancher kubeconfig for %q: %w", ctxName, err)
			}
			internal := source.internalContextName(ctxName)
			return writeSnapshot(cfg, internal, ctxName)
		}
		return "", fmt.Errorf("rancher context %q is not available on any connected server", ctxName)
	}
	cfg, err := m.loadFileConfig()
	if err != nil {
		return "", fmt.Errorf("load kubeconfig for %q: %w", ctxName, err)
	}
	if _, ok := cfg.Contexts[ctxName]; !ok {
		return "", fmt.Errorf("context %q is not present in kubeconfig", ctxName)
	}
	return writeSnapshot(cfg, ctxName, ctxName)
}

// writeSnapshot serializes cfg with current-context set to active, writes it
// atomically to a fresh temp file, and returns the path.
func writeSnapshot(cfg *clientcmdapi.Config, active, ctxName string) (string, error) {
	snapshot := cfg.DeepCopy()
	snapshot.CurrentContext = active
	snapshot.Extensions = nil
	dir, err := os.MkdirTemp("", "kui-kubeconfig-")
	if err != nil {
		return "", fmt.Errorf("create kubeconfig snapshot dir: %w", err)
	}
	path := filepath.Join(dir, "kubeconfig")
	data, err := clientcmd.Write(*snapshot)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("serialize kubeconfig for %q: %w", ctxName, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write kubeconfig snapshot for %q: %w", ctxName, err)
	}
	return path, nil
}
