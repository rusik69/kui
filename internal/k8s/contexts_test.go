package k8s

import (
	"os"
	"path/filepath"
	"testing"
)

const testKubeconfig = `
apiVersion: v1
kind: Config
current-context: dev
clusters:
- name: dev-cluster
  cluster:
    server: https://dev.example.com
- name: prod-cluster
  cluster:
    server: https://prod.example.com
contexts:
- name: dev
  context:
    cluster: dev-cluster
    namespace: default
- name: arn:aws:eks:us-east-1:123456789012:cluster/prod
  context:
    cluster: prod-cluster
users:
- name: dev-user
  user: {}
`

func writeTestKubeconfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatalf("write test kubeconfig: %v", err)
	}
	return path
}

func TestManagerListContexts(t *testing.T) {
	mgr := NewManager(writeTestKubeconfig(t))

	contexts, err := mgr.ListContexts()
	if err != nil {
		t.Fatalf("ListContexts: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %d: %+v", len(contexts), contexts)
	}

	// sorted by name: the ARN-shaped name sorts before "dev"
	if contexts[0].Name != "arn:aws:eks:us-east-1:123456789012:cluster/prod" {
		t.Errorf("unexpected first context: %+v", contexts[0])
	}
	if contexts[1].Name != "dev" || !contexts[1].Current {
		t.Errorf("expected \"dev\" to be the current context, got %+v", contexts[1])
	}
}

func TestManagerDefaultContext(t *testing.T) {
	mgr := NewManager(writeTestKubeconfig(t))

	name, err := mgr.DefaultContext()
	if err != nil {
		t.Fatalf("DefaultContext: %v", err)
	}
	if name != "dev" {
		t.Errorf("expected current-context \"dev\", got %q", name)
	}
}
