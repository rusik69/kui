package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
)

func TestResourceDeleteFailureNamesActionAndResource(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"pods \"demo\" is forbidden: user cannot delete","reason":"Forbidden","details":{"name":"demo","kind":"pods"},"code":403}`)
	}))
	defer apiServer.Close()

	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %q
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`, apiServer.URL)
	kubeconfigPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(kubeconfigPath, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server.mgr = k8s.NewManager(kubeconfigPath)

	req := httptest.NewRequest(http.MethodDelete, "/", nil)
	req.SetPathValue("ctx", encodeCtx("test"))
	req.SetPathValue("ns", "default")
	req.SetPathValue("resource", "pods")
	req.SetPathValue("name", "demo")
	recorder := httptest.NewRecorder()
	server.handleResourceDelete(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d; body: %s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
	for _, want := range []string{`Delete Pod "demo" failed`, "forbidden"} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Errorf("error response %q does not contain %q", recorder.Body.String(), want)
		}
	}
}
