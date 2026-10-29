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

// TestManagerDirectoryOfKubeconfigs verifies that a --kubeconfig directory
// loads and merges every kubeconfig file in it, sorted by file name, and
// that editing a file drops cached clients.
func TestManagerDirectoryOfKubeconfigs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, contextName, server string) {
		t.Helper()
		config := fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: %s
clusters:
- name: %s-cluster
  cluster:
    server: %s
contexts:
- name: %s
  context:
    cluster: %s-cluster
    user: %s-user
users:
- name: %s-user
  user: {}
`, contextName, contextName, server, contextName, contextName, contextName, contextName)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("b-prod.yaml", "prod", "https://prod.example.test")
	write("a-dev.yaml", "dev", "https://dev.example.test")
	// Ignored extensions and hidden files must not be loaded.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a kubeconfig"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden.yaml"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(dir)
	contexts, err := mgr.ListContexts()
	if err != nil {
		t.Fatalf("ListContexts from directory: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts from directory, got %+v", contexts)
	}
	byName := map[string]ContextInfo{}
	for _, c := range contexts {
		byName[c.Name] = c
	}
	dev, ok := byName["dev"]
	if !ok || !dev.Current {
		// a-dev.yaml sorts first, so its current-context wins the merge.
		t.Fatalf("dev context missing or not current: %+v", dev)
	}
	prod, ok := byName["prod"]
	if !ok || prod.Current {
		t.Fatalf("prod context missing or wrongly current: %+v", prod)
	}

	// Clients must resolve per-context hosts across the merged files.
	devClients, err := mgr.Clients("dev")
	if err != nil {
		t.Fatalf("Clients(dev): %v", err)
	}
	if devClients.Config.Host != "https://dev.example.test" {
		t.Fatalf("dev host = %q", devClients.Config.Host)
	}
	prodClients, err := mgr.Clients("prod")
	if err != nil {
		t.Fatalf("Clients(prod): %v", err)
	}
	if prodClients.Config.Host != "https://prod.example.test" {
		t.Fatalf("prod host = %q", prodClients.Config.Host)
	}

	// Adding a file to the directory must invalidate cached clients and
	// surface the new context.
	write("c-staging.yaml", "staging", "https://staging.example.test")
	contexts, err = mgr.ListContexts()
	if err != nil {
		t.Fatalf("ListContexts after adding file: %v", err)
	}
	found := false
	for _, c := range contexts {
		if c.Name == "staging" {
			found = true
		}
	}
	if !found {
		t.Fatalf("staging context not picked up after adding file: %+v", contexts)
	}
	if _, err := mgr.Clients("staging"); err != nil {
		t.Fatalf("Clients(staging) after adding file: %v", err)
	}
	if _, err := mgr.Clients("dev"); err != nil {
		t.Fatalf("cached dev client dropped after unrelated file add: %v", err)
	}

	// Removing a file must drop its contexts and its clients.
	if err := os.Remove(filepath.Join(dir, "c-staging.yaml")); err != nil {
		t.Fatal(err)
	}
	contexts, err = mgr.ListContexts()
	if err != nil {
		t.Fatalf("ListContexts after removing file: %v", err)
	}
	for _, c := range contexts {
		if c.Name == "staging" {
			t.Fatalf("staging context survived file removal: %+v", contexts)
		}
	}
}

// TestManagerDirectoryIgnoresCorruptFiles verifies one unreadable file in the
// directory does not break the other kubeconfigs when Rancher provides a
// fallback... actually verify the client-go behavior is surfaced, then that a
// corrupt file combined with contexts still lists the good ones when the
// corrupt file is removed.
func TestManagerDirectoryWithCorruptFile(t *testing.T) {
	dir := t.TempDir()
	good := `apiVersion: v1
kind: Config
current-context: dev
clusters:
- name: cluster
  cluster:
    server: https://dev.example.test
contexts:
- name: dev
  context:
    cluster: cluster
    user: test
users:
- name: test
  user: {}
`
	if err := os.WriteFile(filepath.Join(dir, "a-dev.yaml"), []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "z-broken.yaml"), []byte("{not yaml"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(dir)
	// client-go aggregates read errors: the corrupt file makes Load fail even
	// though the good file parses. kui must surface that (no silent drop).
	if _, err := mgr.ListContexts(); err == nil {
		t.Fatal("ListContexts accepted a directory containing a corrupt kubeconfig")
	}
	// Once the corrupt file is removed, everything works again.
	if err := os.Remove(filepath.Join(dir, "z-broken.yaml")); err != nil {
		t.Fatal(err)
	}
	contexts, err := mgr.ListContexts()
	if err != nil {
		t.Fatalf("ListContexts after removing corrupt file: %v", err)
	}
	if len(contexts) != 1 || contexts[0].Name != "dev" {
		t.Fatalf("contexts = %+v, want dev", contexts)
	}
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
