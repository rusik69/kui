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

func TestDebugShellStopRouteDeletesLabeledPod(t *testing.T) {
	var deleted bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/namespaces/work/pods/kui-debug-demo" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"kui-debug-demo","namespace":"work","labels":{"kui.dev/ephemeral":"true"}}}`)
		case http.MethodDelete:
			deleted = true
			_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Success","code":200}`)
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer apiServer.Close()

	config := fmt.Sprintf(`apiVersion: v1
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
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(k8s.NewManager(path))
	if err != nil {
		t.Fatal(err)
	}

	url := "http://kui.example/c/" + encodeCtx("test") + "/work/debug-shell/kui-debug-demo/kill"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Origin", "http://kui.example")
	req.Header.Set("X-Kui-CSRF", server.csrfToken)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("stop route status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !deleted {
		t.Fatal("stop route did not delete the labeled debug pod")
	}
	if got := recorder.Header().Get("HX-Trigger"); !strings.Contains(got, `debug pod \"kui-debug-demo\" killed`) {
		t.Errorf("stop route missing success toast: %s", got)
	}
}

func TestDebugShellRequiresConcreteNamespace(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/c/test/_all/debug-shell", nil)
	req.SetPathValue("ctx", "test")
	req.SetPathValue("ns", "_all")
	recorder := httptest.NewRecorder()

	server.handleDebugShell(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("debug shell status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}
