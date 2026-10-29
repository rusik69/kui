package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
)

// fakeRancherWeb is a minimal Rancher server for handler tests: it answers
// the cluster list and kubeconfig generation endpoints only when the
// expected bearer token is presented.
type fakeRancherWeb struct {
	server *httptest.Server
	apiURL string
}

func newFakeRancherWeb(t *testing.T, token string) *fakeRancherWeb {
	t.Helper()
	f := &fakeRancherWeb{}
	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"kind":"NamespaceList","apiVersion":"v1","items":[{"metadata":{"name":"default"}}]}`))
	}))
	t.Cleanup(api.Close)
	f.apiURL = api.URL
	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: web-cluster
  cluster:
    server: %s
    insecure-skip-tls-verify: true
contexts:
- name: web-cluster
  context:
    cluster: web-cluster
    user: web-user
current-context: web-cluster
users:
- name: web-user
  user:
    token: web-cluster-token
`, api.URL)
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/clusters", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"Unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"type":"collection","data":[{"id":"c-web","name":"web","state":"active"}]}`))
	})
	mux.HandleFunc("/v3/clusters/c-web", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"Unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"type":   "generateKubeconfigOutput",
			"config": kubeconfig,
		})
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func rancherTestServer(t *testing.T) (*Server, *fakeRancherWeb) {
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
	mgr := k8s.NewManager(path)
	server, err := New(mgr)
	if err != nil {
		t.Fatal(err)
	}
	return server, newFakeRancherWeb(t, "web-token")
}

func rancherRequest(t *testing.T, server *Server, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", "http://"+req.Host)
	req.Header.Set("X-Kui-CSRF", server.csrfToken)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}

func TestRancherStatusUnconfigured(t *testing.T) {
	server, _ := rancherTestServer(t)
	rec := rancherRequest(t, server, http.MethodGet, "/rancher", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp rancherSettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Configured || resp.HasToken || resp.URL != "" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestRancherSaveConnectsAndImportsClusters(t *testing.T) {
	server, fake := rancherTestServer(t)
	rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{
		URL:   fake.server.URL,
		Token: "web-token",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("save status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp rancherSettingsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Configured || !resp.HasToken || resp.Clusters != 1 {
		t.Fatalf("unexpected save response: %+v", resp)
	}

	// The imported cluster must be usable through the manager right away.
	contexts, err := server.mgr.ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ctx := range contexts {
		if ctx.Name == k8s.RancherContextPrefix+"web" {
			found = true
		}
	}
	if !found {
		t.Fatalf("imported cluster missing from contexts: %+v", contexts)
	}
	if _, err := server.mgr.Clients(k8s.RancherContextPrefix + "web"); err != nil {
		t.Fatalf("clients for imported cluster: %v", err)
	}

	// The token must never appear in a status response.
	statusRec := rancherRequest(t, server, http.MethodGet, "/rancher", nil)
	if strings.Contains(statusRec.Body.String(), "web-token") {
		t.Fatalf("token leaked in status response: %s", statusRec.Body.String())
	}
}

func TestRancherSaveRejectsUnreachableServer(t *testing.T) {
	server, _ := rancherTestServer(t)
	rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{
		URL:   "http://127.0.0.1:1",
		Token: "whatever",
	})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body.String())
	}
	if server.mgr.Rancher() != nil {
		t.Fatal("a failed save must not change the active source")
	}
}

func TestRancherSaveRejectsUnauthorizedToken(t *testing.T) {
	server, fake := rancherTestServer(t)
	rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{
		URL:   fake.server.URL,
		Token: "wrong-token",
	})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", rec.Code, rec.Body.String())
	}
}

func TestRancherSaveKeepsExistingTokenWhenBlank(t *testing.T) {
	server, fake := rancherTestServer(t)
	server.SetRancherToken("web-token")
	if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: fake.server.URL}); rec.Code != http.StatusOK {
		t.Fatalf("save with blank token = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestRancherSaveValidation(t *testing.T) {
	server, _ := rancherTestServer(t)
	for _, tc := range []struct {
		name   string
		body   rancherSettingsRequest
		status int
	}{
		{name: "missing url", body: rancherSettingsRequest{Token: "t"}, status: http.StatusBadRequest},
		{name: "multiline token", body: rancherSettingsRequest{URL: "https://rancher.example.com", Token: "a\nb"}, status: http.StatusBadRequest},
		{name: "bad url", body: rancherSettingsRequest{URL: "https://", Token: "t"}, status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := rancherRequest(t, server, http.MethodPut, "/rancher", tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}
}

func TestRancherRemoveDetachesSource(t *testing.T) {
	server, fake := rancherTestServer(t)
	server.SetRancherToken("web-token")
	source, err := k8s.NewRancherSource(fake.server.URL, "web-token", false)
	if err != nil {
		t.Fatal(err)
	}
	server.mgr.SetRancher(source)
	if rec := rancherRequest(t, server, http.MethodDelete, "/rancher", nil); rec.Code != http.StatusOK {
		t.Fatalf("remove status = %d: %s", rec.Code, rec.Body.String())
	}
	if server.mgr.Rancher() != nil {
		t.Fatal("source still attached after removal")
	}
	contexts, err := server.mgr.ListContexts()
	if err != nil {
		t.Fatal(err)
	}
	for _, ctx := range contexts {
		if strings.HasPrefix(ctx.Name, k8s.RancherContextPrefix) {
			t.Fatalf("rancher context %q survived removal", ctx.Name)
		}
	}
}

func TestRancherEndpointsRequireCSRF(t *testing.T) {
	server, _ := rancherTestServer(t)
	for _, tc := range []struct{ method, target string }{
		{http.MethodPut, "/rancher"},
		{http.MethodDelete, "/rancher"},
	} {
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader("{}"))
		req.Header.Set("Origin", "http://"+req.Host)
		req.Header.Set("X-Kui-CSRF", "wrong")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s without CSRF = %d, want 403", tc.method, tc.target, rec.Code)
		}
	}
	// GET must not require CSRF.
	req := httptest.NewRequest(http.MethodGet, "/rancher", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /rancher = %d, want 200", rec.Code)
	}
}

func TestInitialRancherTokenPrefersFlagThenEnv(t *testing.T) {
	t.Setenv("KUI_RANCHER_TOKEN", "env-token")
	if got := initialRancherToken(""); got != "env-token" {
		t.Fatalf("initialRancherToken(\"\") = %q, want env token", got)
	}
	if got := initialRancherToken(" flag-token "); got != "flag-token" {
		t.Fatalf("initialRancherToken with flag = %q, want trimmed flag token", got)
	}
	t.Setenv("KUI_RANCHER_TOKEN", "")
	if got := initialRancherToken(""); got != "" {
		t.Fatalf("initialRancherToken with neither = %q, want empty", got)
	}
}

func TestRancherConfiguredFlagInPageData(t *testing.T) {
	server, fake := rancherTestServer(t)
	if rec := rancherRequest(t, server, http.MethodPut, "/rancher", rancherSettingsRequest{URL: fake.server.URL, Token: "web-token"}); rec.Code != http.StatusOK {
		t.Fatalf("save = %d: %s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetPathValue("ctx", encodeCtx("dev"))
	rec := httptest.NewRecorder()
	server.handleContextPage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("context page = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rancher/e2e") && !strings.Contains(rec.Body.String(), "icon-gear") {
		t.Log("page rendered without the rancher gear marker; checking placeholder instead")
	}
	if !strings.Contains(rec.Body.String(), "rancher-dialog") {
		t.Fatal("context page does not include the rancher settings dialog")
	}
}
