package k8s

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

const snapshotKubeconfig = `apiVersion: v1
kind: Config
current-context: dev
clusters:
- name: local
  cluster:
    server: https://127.0.0.1:1
contexts:
- name: dev
  context:
    cluster: local
    user: local
- name: qa
  context:
    cluster: local
    user: local
users:
- name: local
  user:
    token: tok
`

func snapshotTestManager(t *testing.T) *Manager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(snapshotKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return NewManager(path)
}

func TestSnapshotKubeconfigFileContext(t *testing.T) {
	m := snapshotTestManager(t)
	snapshot, err := m.SnapshotKubeconfig("dev")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(snapshot))
	info, err := os.Stat(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("snapshot mode = %o, want 600", perm)
	}
	cfg, err := clientcmd.LoadFromFile(snapshot)
	if err != nil {
		t.Fatalf("snapshot is not a loadable kubeconfig: %v", err)
	}
	if cfg.CurrentContext != "dev" {
		t.Fatalf("current-context = %q, want dev", cfg.CurrentContext)
	}
	// Both contexts survive; the snapshot is a full copy, current pinned.
	if len(cfg.Contexts) != 2 {
		t.Fatalf("contexts = %d, want 2", len(cfg.Contexts))
	}
}

func TestSnapshotKubeconfigUnknownContext(t *testing.T) {
	m := snapshotTestManager(t)
	if _, err := m.SnapshotKubeconfig("missing"); err == nil {
		t.Fatal("snapshot for unknown context must fail")
	}
}
