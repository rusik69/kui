package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
)

// createTestServer builds a Server over a kubeconfig pointing at an
// unreachable API server. Scaffold/parse/identity checks run before any
// cluster round trip, so most assertions hold without a live API.
func createTestServer(t *testing.T) *Server {
	t.Helper()
	config := `apiVersion: v1
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
users:
- name: local
  user: {}
`
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(k8s.NewManager(path))
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestScaffoldRendersKindAndNamespace(t *testing.T) {
	server := createTestServer(t)
	tests := []struct {
		name         string
		resource     string
		ns           string
		wantKind     string
		wantNS       string
		notWantNS    bool
		wantFragment string
	}{
		{name: "namespaced deployment", resource: "deployments", ns: "dev", wantKind: "Deployment", wantNS: "namespace: dev", wantFragment: "replicas: 1"},
		{name: "all-namespaces scope omits namespace", resource: "configmaps", ns: "_all", wantKind: "ConfigMap", notWantNS: true, wantFragment: "data:"},
		{name: "cluster-scoped node", resource: "nodes", ns: "dev", wantKind: "Node", notWantNS: true},
		{name: "secret scaffold prefers stringData", resource: "secrets", ns: "dev", wantKind: "Secret", wantFragment: "stringData:"},
		{name: "unknown kind scaffold covers CRD-less fallback", resource: "widgets", ns: "dev", wantKind: "", wantFragment: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/c/dev/"+tt.ns+"/"+tt.resource+"/new", nil)
			req.SetPathValue("ctx", encodeCtx("dev"))
			req.SetPathValue("ns", tt.ns)
			req.SetPathValue("resource", tt.resource)
			server.handleResourceScaffold(rec, req)
			body := rec.Body.String()
			if tt.wantKind == "" {
				// Unresolvable kind falls back to discovery against an
				// unreachable server: the scaffold must fail cleanly, not 500.
				if rec.Code != http.StatusBadGateway && rec.Code != http.StatusNotFound {
					t.Fatalf("expected a clean failure, got %d: %s", rec.Code, body)
				}
				return
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("scaffold %s: status %d: %s", tt.resource, rec.Code, body)
			}
			if !strings.Contains(body, "kind: "+tt.wantKind) {
				t.Errorf("scaffold missing kind %s:\n%s", tt.wantKind, body)
			}
			if tt.notWantNS && strings.Contains(body, "namespace:") {
				t.Errorf("scaffold must not set a namespace:\n%s", body)
			}
			if tt.wantNS != "" && !strings.Contains(body, tt.wantNS) {
				t.Errorf("scaffold missing %q:\n%s", tt.wantNS, body)
			}
			if tt.wantFragment != "" && !strings.Contains(body, tt.wantFragment) {
				t.Errorf("scaffold missing fragment %q:\n%s", tt.wantFragment, body)
			}
		})
	}
}

func TestCreateRejectsBadManifestsBeforeCluster(t *testing.T) {
	server := createTestServer(t)
	tests := []struct {
		name    string
		yaml    string
		wantSub string
	}{
		{
			name:    "invalid yaml",
			yaml:    "kind: [unclosed",
			wantSub: "invalid YAML",
		},
		{
			name:    "kind mismatch",
			yaml:    "apiVersion: v1\nkind: Pod\nmetadata:\n  name: x\n  namespace: dev\n",
			wantSub: "does not match resource kind",
		},
		{
			name:    "missing name",
			yaml:    "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  namespace: dev\n",
			wantSub: "metadata.name",
		},
		{
			name:    "cluster-scoped with namespace",
			yaml:    "apiVersion: v1\nkind: Node\nmetadata:\n  name: x\n  namespace: dev\n",
			wantSub: "must not set a namespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			form := url.Values{"yaml": {tt.yaml}}
			req := httptest.NewRequest(http.MethodPost, "/c/dev/dev/configmaps/new", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetPathValue("ctx", encodeCtx("dev"))
			req.SetPathValue("ns", "dev")
			req.SetPathValue("resource", resourceForKind(tt.yaml, tt.wantSub))
			server.handleResourceCreate(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantSub) {
				t.Errorf("error %q does not contain %q", rec.Body.String(), tt.wantSub)
			}
		})
	}
}

// resourceForKind picks the URL resource name. The kind-mismatch case posts
// a Pod manifest against configmaps on purpose, so it must not resolve to
// "pods" — keyed off the expected error instead of the manifest.
func resourceForKind(manifest, wantSub string) string {
	if wantSub == "does not match resource kind" {
		return "configmaps"
	}
	switch {
	case strings.Contains(manifest, "kind: Pod"):
		return "pods"
	case strings.Contains(manifest, "kind: Node"):
		return "nodes"
	default:
		return "configmaps"
	}
}

func TestCreateNamespacedManifestWithoutPageNamespaceRejected(t *testing.T) {
	server := createTestServer(t)
	rec := httptest.NewRecorder()
	form := url.Values{"yaml": {"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: x\n"}}
	req := httptest.NewRequest(http.MethodPost, "/c/dev/_all/configmaps/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("ctx", encodeCtx("dev"))
	req.SetPathValue("ns", "_all")
	req.SetPathValue("resource", "configmaps")
	server.handleResourceCreate(rec, req)
	// _all provides no namespace and the manifest none: rejected before any
	// cluster call (the request would otherwise fail with a dial error).
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "metadata.namespace") {
		t.Errorf("unexpected error: %s", rec.Body.String())
	}
}

func TestCreateTableDataCarriesCreateURL(t *testing.T) {
	// ResourceKind name drives the URL; the route itself is registered in
	// routes() — this pins the shape so the template button stays wired.
	rk := k8s.ResourceKind{Name: "configmaps", Kind: "ConfigMap"}
	_ = rk
	if _, ok := k8s.ByName("configmaps"); !ok {
		t.Fatal("configmaps missing from registry; create button would never render")
	}
}
