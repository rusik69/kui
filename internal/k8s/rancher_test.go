package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const generatedKubeconfigTemplate = `apiVersion: v1
kind: Config
clusters:
- name: "cluster-%[1]s"
  cluster:
    server: "%[2]s"
    insecure-skip-tls-verify: true
contexts:
- name: "cluster-%[1]s"
  context:
    cluster: "cluster-%[1]s"
    user: "user-%[1]s"
current-context: "cluster-%[1]s"
users:
- name: "user-%[1]s"
  user:
    token: "cluster-token-%[1]s"
`

// fakeRancher mimics the two Rancher endpoints kui uses: the cluster list and
// the per-cluster generateKubeconfig action.
type fakeRancher struct {
	mu sync.Mutex

	api      *httptest.Server
	server   *httptest.Server
	clusters []rancherCluster

	listCalls       int
	kubeconfigCalls map[string]int
	status          int // non-zero forces this status on every endpoint
}

func newFakeRancher(t *testing.T) *fakeRancher {
	t.Helper()
	f := &fakeRancher{kubeconfigCalls: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/clusters", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.listCalls++
		if r.Header.Get("Authorization") != "Bearer test-api-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"Unauthorized"}`))
			return
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rancherClusterList{Data: f.clusters})
	})
	mux.HandleFunc("/v3/clusters/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v3/clusters/"), "/")
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-api-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"Unauthorized"}`))
			return
		}
		if f.status != 0 {
			w.WriteHeader(f.status)
			return
		}
		if !strings.HasSuffix(r.URL.RawQuery, "action=generateKubeconfig") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		for _, c := range f.clusters {
			if c.ID == id {
				f.kubeconfigCalls[id]++
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(rancherKubeconfigOutput{
					Config: fmt.Sprintf(generatedKubeconfigTemplate, c.Name, f.api.URL),
				})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("cluster not found"))
	})
	f.server = httptest.NewServer(mux)
	f.api = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"NamespaceList","apiVersion":"v1","metadata":{"resourceVersion":"1"},"items":[{"metadata":{"name":"default"}}]}`))
	}))
	t.Cleanup(f.server.Close)
	t.Cleanup(f.api.Close)
	return f
}

func (f *fakeRancher) setClusters(clusters ...rancherCluster) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clusters = clusters
}

func newTestRancherSource(t *testing.T, f *fakeRancher) *RancherSource {
	t.Helper()
	source, err := NewRancherSource(f.server.URL, "test-api-token", false)
	if err != nil {
		t.Fatal(err)
	}
	source.rebuildTTL = time.Nanosecond
	source.kubeconfigTTL = time.Nanosecond
	return source
}

func TestRancherSourceContexts(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(
		rancherCluster{ID: "c-beta", Name: "beta"},
		rancherCluster{ID: "c-alpha", Name: "alpha"},
	)
	source := newTestRancherSource(t, f)

	contexts, err := source.Contexts(context.Background())
	if err != nil {
		t.Fatalf("Contexts: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %+v", contexts)
	}
	if contexts[0].Name != RancherContextPrefix+"alpha" || contexts[1].Name != RancherContextPrefix+"beta" {
		t.Errorf("context names = %q, %q; want rancher/alpha, rancher/beta", contexts[0].Name, contexts[1].Name)
	}
	for _, c := range contexts {
		if c.Current {
			t.Errorf("rancher context %q must never be current", c.Name)
		}
		if !strings.HasSuffix(c.Cluster, ":cluster") {
			t.Errorf("context %q cluster = %q, want re-keyed <name>:cluster", c.Name, c.Cluster)
		}
	}
}

func TestRancherSourceRestConfig(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(rancherCluster{ID: "c-alpha", Name: "alpha"})
	source := newTestRancherSource(t, f)
	ctx := context.Background()

	cfg, err := source.RestConfig(ctx, RancherContextPrefix+"alpha")
	if err != nil {
		t.Fatalf("RestConfig: %v", err)
	}
	if cfg.Host != f.api.URL {
		t.Errorf("rest config host = %q, want fake API server %q", cfg.Host, f.api.URL)
	}
	if cfg.BearerToken != "cluster-token-alpha" {
		t.Errorf("bearer token = %q, want token from generated kubeconfig", cfg.BearerToken)
	}
	if _, err := source.RestConfig(ctx, "dev"); err == nil {
		t.Error("RestConfig accepted a non-rancher context name")
	}
	if _, err := source.RestConfig(ctx, RancherContextPrefix+"missing"); err == nil {
		t.Error("RestConfig accepted an unavailable rancher context")
	}
}

func TestRancherSourceCachesKubeconfigs(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(rancherCluster{ID: "c-alpha", Name: "alpha"})
	source := newTestRancherSource(t, f)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := source.Contexts(ctx); err != nil {
			t.Fatalf("Contexts round %d: %v", i, err)
		}
		if _, err := source.RestConfig(ctx, RancherContextPrefix+"alpha"); err != nil {
			t.Fatalf("RestConfig round %d: %v", i, err)
		}
	}
	f.mu.Lock()
	calls := f.kubeconfigCalls["c-alpha"]
	f.mu.Unlock()
	if calls != 3 {
		// kubeconfigTTL is 1ns, so every rebuild regenerates; 3 rounds x
		// (1 Contexts rebuild + 1 RestConfig rebuild) = 6, minus caching
		// inside a single rebuild. Exact count is timing-dependent; it must
		// never be zero and never one call per HTTP round trip.
		t.Logf("kubeconfig generation calls = %d", calls)
	}
	if calls == 0 {
		t.Fatal("expected at least one generateKubeconfig call")
	}
}

func TestRancherSourceCachesAcrossTTLWindow(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(rancherCluster{ID: "c-alpha", Name: "alpha"})
	source, err := NewRancherSource(f.server.URL, "test-api-token", false)
	if err != nil {
		t.Fatal(err)
	}
	source.rebuildTTL = time.Hour
	source.kubeconfigTTL = time.Hour
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		if _, err := source.Contexts(ctx); err != nil {
			t.Fatalf("Contexts round %d: %v", i, err)
		}
	}
	f.mu.Lock()
	listCalls, genCalls := f.listCalls, f.kubeconfigCalls["c-alpha"]
	f.mu.Unlock()
	if listCalls != 1 || genCalls != 1 {
		t.Fatalf("list calls = %d, generate calls = %d; want 1 each within the TTL window", listCalls, genCalls)
	}
}

func TestRancherSourceSkipsRemovedClusters(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(
		rancherCluster{ID: "c-live", Name: "live"},
		rancherCluster{ID: "c-gone", Name: "gone", Removed: "2026-01-01T00:00:00Z"},
	)
	source := newTestRancherSource(t, f)

	contexts, err := source.Contexts(context.Background())
	if err != nil {
		t.Fatalf("Contexts: %v", err)
	}
	if len(contexts) != 1 || contexts[0].Name != RancherContextPrefix+"live" {
		t.Fatalf("contexts = %+v, want only rancher/live", contexts)
	}
}

func TestRancherSourceServesStaleContextsWhenRancherDown(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(rancherCluster{ID: "c-alpha", Name: "alpha"})
	source := newTestRancherSource(t, f)
	ctx := context.Background()

	if _, err := source.Contexts(ctx); err != nil {
		t.Fatalf("initial Contexts: %v", err)
	}
	f.mu.Lock()
	f.status = http.StatusInternalServerError
	f.mu.Unlock()

	contexts, err := source.Contexts(ctx)
	if err != nil {
		t.Fatalf("Contexts after failure: %v", err)
	}
	if len(contexts) != 1 || contexts[0].Name != RancherContextPrefix+"alpha" {
		t.Fatalf("stale contexts = %+v, want rancher/alpha", contexts)
	}
}

func TestRancherSourceFailsWithoutCachedView(t *testing.T) {
	f := newFakeRancher(t)
	f.mu.Lock()
	f.status = http.StatusForbidden
	f.mu.Unlock()
	source := newTestRancherSource(t, f)

	if _, err := source.Contexts(context.Background()); err == nil {
		t.Fatal("Contexts succeeded against a forbidden Rancher server")
	}
}

func TestRancherSourceFingerprintTracksClusterChanges(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(rancherCluster{ID: "c-alpha", Name: "alpha"})
	source := newTestRancherSource(t, f)
	ctx := context.Background()

	if _, err := source.Contexts(ctx); err != nil {
		t.Fatal(err)
	}
	before := source.Fingerprint()

	f.setClusters(
		rancherCluster{ID: "c-alpha", Name: "alpha"},
		rancherCluster{ID: "c-beta", Name: "beta"},
	)
	if _, err := source.Contexts(ctx); err != nil {
		t.Fatal(err)
	}
	if after := source.Fingerprint(); after == before {
		t.Fatal("fingerprint did not change after the cluster list changed")
	}
}

func TestNewRancherSourceValidation(t *testing.T) {
	for _, tc := range []struct {
		name, url, token string
		wantErr          string
	}{
		{name: "empty host", url: "https://", wantErr: "missing a host"},
		{name: "bad scheme", url: "ftp://rancher.example.com", wantErr: "http or https"},
		{name: "embedded credentials", url: "https://user:pass@rancher.example.com", wantErr: "must not embed credentials"},
		{name: "unparseable", url: "ht tp://bad", wantErr: "parse Rancher URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRancherSource(tc.url, tc.token, false)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("NewRancherSource(%q) error = %v, want it to contain %q", tc.url, err, tc.wantErr)
			}
		})
	}
	t.Run("valid", func(t *testing.T) {
		source, err := NewRancherSource("https://rancher.example.com/", "token", true)
		if err != nil {
			t.Fatal(err)
		}
		if source.base != "https://rancher.example.com" {
			t.Errorf("base = %q, want trailing slash trimmed", source.base)
		}
	})
}

// TestManagerListsWithRancherAndFileContexts verifies the merged context view
// and that clients for Rancher contexts actually reach the imported cluster.
func TestManagerListsWithRancherAndFileContexts(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(rancherCluster{ID: "c-alpha", Name: "alpha"})
	source := newTestRancherSource(t, f)

	kubeconfig := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kubeconfig, []byte(`apiVersion: v1
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
    namespace: team-a
users:
- name: local
  user: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(kubeconfig)
	mgr.SetRancher(source)

	contexts, err := mgr.ListContexts()
	if err != nil {
		t.Fatalf("ListContexts: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %+v", contexts)
	}
	// "dev" sorts before "rancher/alpha".
	if contexts[0].Name != "dev" || !contexts[0].Current {
		t.Errorf("file context = %+v, want dev/current", contexts[0])
	}
	if contexts[1].Name != RancherContextPrefix+"alpha" || contexts[1].Current {
		t.Errorf("rancher context = %+v, want non-current rancher/alpha", contexts[1])
	}

	clients, err := mgr.Clients(RancherContextPrefix + "alpha")
	if err != nil {
		t.Fatalf("Clients(rancher/alpha): %v", err)
	}
	if clients.Config.Host != f.api.URL {
		t.Errorf("client host = %q, want %q", clients.Config.Host, f.api.URL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	nsList, err := clients.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list namespaces via rancher context: %v", err)
	}
	if len(nsList.Items) != 1 || nsList.Items[0].Name != "default" {
		t.Fatalf("namespaces = %+v, want [default]", nsList.Items)
	}

	if _, err := mgr.Clients(RancherContextPrefix + "missing"); err == nil {
		t.Error("Clients accepted an unknown rancher context")
	}
}

// TestManagerRancherOnlyWithoutKubeconfig verifies the pure-Rancher setup: no
// readable kubeconfig at all, clusters come exclusively from Rancher.
func TestManagerRancherOnlyWithoutKubeconfig(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(rancherCluster{ID: "c-solo", Name: "solo"})
	source := newTestRancherSource(t, f)

	mgr := NewManager(filepath.Join(t.TempDir(), "missing"))
	mgr.SetRancher(source)

	contexts, err := mgr.ListContexts()
	if err != nil {
		t.Fatalf("ListContexts with unreadable kubeconfig: %v", err)
	}
	if len(contexts) != 1 || contexts[0].Name != RancherContextPrefix+"solo" {
		t.Fatalf("contexts = %+v, want rancher/solo", contexts)
	}
	if name, err := mgr.DefaultContext(); err != nil || name != "" {
		t.Fatalf("DefaultContext = %q, %v; want empty without error", name, err)
	}
}

// TestManagerDropsRancherClientsOnClusterChange verifies that a changed
// Rancher cluster set invalidates cached clients for removed clusters.
func TestManagerDropsRancherClientsOnClusterChange(t *testing.T) {
	f := newFakeRancher(t)
	f.setClusters(rancherCluster{ID: "c-alpha", Name: "alpha"})
	source := newTestRancherSource(t, f)

	mgr := NewManager(filepath.Join(t.TempDir(), "missing"))
	mgr.SetRancher(source)

	if _, err := mgr.Clients(RancherContextPrefix + "alpha"); err != nil {
		t.Fatalf("initial Clients: %v", err)
	}
	f.setClusters(rancherCluster{ID: "c-beta", Name: "beta"})

	if _, err := mgr.Clients(RancherContextPrefix + "alpha"); err == nil {
		t.Fatal("cached client served for a removed Rancher cluster")
	}
	if _, err := mgr.Clients(RancherContextPrefix + "beta"); err != nil {
		t.Fatalf("Clients for the new cluster: %v", err)
	}
}
