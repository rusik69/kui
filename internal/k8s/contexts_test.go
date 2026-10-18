package k8s

import (
	"fmt"
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

func TestManagerReloadsKubeconfigAndDropsStaleClients(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	write := func(contextName, server string) {
		t.Helper()
		config := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: %s
clusters:
- name: cluster
  cluster:
    server: %s
contexts:
- name: %s
  context:
    cluster: cluster
    user: test
users:
- name: test
  user: {}
`, contextName, server, contextName)
		if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("dev", "https://dev.example.test")
	manager := NewManager(path)
	dev, err := manager.Clients("dev")
	if err != nil {
		t.Fatal(err)
	}
	if dev.Config.Host != "https://dev.example.test" {
		t.Fatalf("initial client host = %q", dev.Config.Host)
	}

	write("prod", "https://prod.example.test")
	contexts, err := manager.ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	if len(contexts) != 1 || contexts[0].Name != "prod" || !contexts[0].Current {
		t.Fatalf("reloaded contexts = %+v", contexts)
	}
	if _, err := manager.Clients("dev"); err == nil {
		t.Fatal("removed context unexpectedly returned its cached client")
	}
	prod, err := manager.Clients("prod")
	if err != nil {
		t.Fatal(err)
	}
	if prod.Config.Host != "https://prod.example.test" {
		t.Fatalf("reloaded client host = %q, want prod API server", prod.Config.Host)
	}
}
