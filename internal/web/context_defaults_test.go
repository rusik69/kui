package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
)

func contextDefaultsServer(t *testing.T) *Server {
	t.Helper()
	config := `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: http://127.0.0.1:9
contexts:
- name: dev
  context:
    cluster: test
    user: test
    namespace: development
- name: prod
  context:
    cluster: test
    user: test
    namespace: production
current-context: dev
users:
- name: test
  user: {}
`
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server.mgr = k8s.NewManager(path)
	return server
}

func TestConfiguredContextAndNamespaceDefaults(t *testing.T) {
	server := contextDefaultsServer(t)
	server.SetDefaults("prod", "custom")

	index := httptest.NewRecorder()
	server.handleIndex(index, httptest.NewRequest(http.MethodGet, "/", nil))
	if index.Code != http.StatusFound || index.Header().Get("Location") != "/c/"+encodeCtx("prod") {
		t.Fatalf("root redirect = %d %q, want configured prod context", index.Code, index.Header().Get("Location"))
	}

	data, err := server.buildPageData(context.Background(), encodeCtx("prod"), "", "overview")
	if err != nil {
		t.Fatal(err)
	}
	if data.Namespace != "custom" {
		t.Fatalf("default namespace = %q, want custom", data.Namespace)
	}
}

func TestStaleContextPageRedirectsToConfiguredContext(t *testing.T) {
	server := contextDefaultsServer(t)
	server.SetDefaults("prod", "")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetPathValue("ctx", encodeCtx("removed"))
	recorder := httptest.NewRecorder()
	server.handleContextPage(recorder, req)
	want := "/c/" + encodeCtx("prod")
	if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != want {
		t.Fatalf("stale context response = %d %q, want redirect to %s: %s", recorder.Code, recorder.Header().Get("Location"), want, recorder.Body.String())
	}
}

func TestContextPageRedirectsMalformedAndMissingContextIDs(t *testing.T) {
	server := contextDefaultsServer(t)
	server.SetDefaults("prod", "")
	for _, ctxID := range []string{"invalid!", encodeCtx("missing")} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.SetPathValue("ctx", ctxID)
		recorder := httptest.NewRecorder()
		server.handleContextPage(recorder, req)
		want := "/c/" + encodeCtx("prod")
		if recorder.Code != http.StatusFound || recorder.Header().Get("Location") != want {
			t.Errorf("context %q response = %d %q, want redirect to %q", ctxID, recorder.Code, recorder.Header().Get("Location"), want)
		}
	}
}
