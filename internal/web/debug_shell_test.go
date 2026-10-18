package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestNodeDebugShellCreatesReadyNodePodAndRendersSession(t *testing.T) {
	var submitted corev1.Pod
	created := &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{Name: "kui-node-shell-abc", Namespace: "ops"},
		Status: corev1.PodStatus{
			Phase:             corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{Name: "shell", Ready: true}},
		},
	}
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces/ops/pods":
			_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"PodList","items":[]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/namespaces/ops/pods":
			if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
				t.Errorf("decode created pod: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			created.Labels = submitted.Labels
			created.Spec = submitted.Spec
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(created)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces/ops/pods/kui-node-shell-abc":
			_ = json.NewEncoder(w).Encode(created)
		default:
			http.Error(w, "unexpected Kubernetes API request: "+r.Method+" "+r.URL.String(), http.StatusNotFound)
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

	url := "http://kui.example/c/" + encodeCtx("test") + "/ops/nodes/worker-1/debug-shell"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	req.Header.Set("Origin", "http://kui.example")
	req.Header.Set("X-Kui-CSRF", server.csrfToken)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("node shell status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if submitted.Spec.NodeName != "worker-1" || !submitted.Spec.HostPID || !submitted.Spec.HostIPC || !submitted.Spec.HostNetwork {
		t.Fatalf("created node shell pod spec = %+v", submitted.Spec)
	}
	if !strings.Contains(recorder.Body.String(), "Node shell") ||
		!strings.Contains(recorder.Body.String(), "/c/"+encodeCtx("test")+"/ops/pods/kui-node-shell-abc/exec") ||
		!strings.Contains(recorder.Body.String(), "/host") {
		t.Fatalf("node shell response missing session details: %s", recorder.Body.String())
	}
}

func TestNodeDebugShellRequiresConcreteNamespace(t *testing.T) {
	for _, namespace := range []string{"_all", "_", ""} {
		t.Run(namespace, func(t *testing.T) {
			server := &Server{}
			req := httptest.NewRequest(http.MethodPost, "/c/test/"+namespace+"/nodes/worker-1/debug-shell", nil)
			req.SetPathValue("ctx", "test")
			req.SetPathValue("ns", namespace)
			req.SetPathValue("node", "worker-1")
			recorder := httptest.NewRecorder()

			server.handleNodeDebugShell(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("node shell status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
		})
	}
}
