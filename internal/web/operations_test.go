package web

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
)

func TestHealthAndReadinessEndpoints(t *testing.T) {
	config := `apiVersion: v1
kind: Config
contexts:
- name: dev
  context:
    cluster: local
    user: local
clusters:
- name: local
  cluster:
    server: https://127.0.0.1:1
users:
- name: local
  user: {}
`
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(k8s.NewManager(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/healthz", http.StatusOK},
		{"/readyz", http.StatusServiceUnavailable},
	} {
		r := httptest.NewRecorder()
		s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if r.Code != tc.code {
			t.Errorf("GET %s status = %d, want %d", tc.path, r.Code, tc.code)
		}
	}
	s.SetDefaults("dev", "")
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("ready status = %d, want %d: %s", r.Code, http.StatusOK, r.Body.String())
	}
}

func TestAccessLogOmitsQueryString(t *testing.T) {
	var logs bytes.Buffer
	handler := WithAccessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}), log.New(&logs, "", 0))
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/search?token=secret", nil))
	if r.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", r.Code, http.StatusAccepted)
	}
	if got := logs.String(); !strings.Contains(got, "GET /search 202") || strings.Contains(got, "secret") || strings.Contains(got, "token=") {
		t.Fatalf("unexpected access log: %q", got)
	}
}
