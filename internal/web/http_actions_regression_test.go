package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/rusik69/kui/internal/k8s"
)

type nonFlushingWriter struct {
	header http.Header
	status int
	body   strings.Builder
}

func (w *nonFlushingWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *nonFlushingWriter) WriteHeader(status int)      { w.status = status }
func (w *nonFlushingWriter) Write(p []byte) (int, error) { return w.body.Write(p) }

func TestWatchUnsupportedWriterGetsSSEProtocolError(t *testing.T) {
	w := &nonFlushingWriter{}
	req := httptest.NewRequest(http.MethodGet, "/watch", nil)
	(&Server{}).handleWatch(w, req)
	if w.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.status, http.StatusInternalServerError)
	}
	if got := w.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", got)
	}
	if got := w.body.String(); !strings.Contains(got, "event: error") || !strings.Contains(got, "streaming unsupported") {
		t.Fatalf("body = %q, want protocol error event", got)
	}
}

func TestStatusSetupFailureIsHTTPError(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req.SetPathValue("ctx", "invalid-base64!")
	rec := httptest.NewRecorder()
	(&Server{}).handleStatus(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("content type = %q, want text/plain", got)
	}
	if got := rec.Body.String(); !strings.Contains(got, "Unreachable") {
		t.Fatalf("body = %q, want Unreachable", got)
	}
}

func TestPortForwardRejectsInvalidAndOversizedFormsBeforeClientLookup(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"zero", "localPort=0&remotePort=80"},
		{"too-large", "localPort=65536&remotePort=80"},
		{"negative-remote", "localPort=80&remotePort=-1"},
		{"body-limit", "localPort=80&remotePort=80&padding=" + strings.Repeat("x", 2048)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/portforward", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.SetPathValue("ctx", "invalid-base64!")
			rec := httptest.NewRecorder()
			(&Server{}).handlePortForwardStart(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestMetricsScriptEscapesScriptContextCharacters(t *testing.T) {
	rec := httptest.NewRecorder()
	writeMetricsScript(rec, 0, 0, map[string]rowUsage{"</script><script>alert(1)</script>": {CPU: "1m", Mem: "1Mi"}})
	body := rec.Body.String()
	if strings.Contains(body, "</script><script>") || !strings.Contains(body, `\u003c/script\u003e`) {
		t.Fatalf("script payload not context escaped: %s", body)
	}
}

func TestWaitForDebugPodRequiresReadyShellContainer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	result := make(chan error, 1)
	go func() {
		result <- waitForDebugPod(ctx, func(context.Context) (*corev1.Pod, error) {
			calls++
			if calls == 1 {
				return &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "shell", Ready: false}}}}, nil
			}
			return &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "shell", Ready: true}}}}, nil
		})
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("waitForDebugPod() error = %v", err)
		}
		if calls < 2 {
			t.Fatalf("readiness returned after %d poll(s)", calls)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readiness did not complete")
	}
}

func TestWaitForDebugPodReturnsTerminatedContainerAsNotReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- waitForDebugPod(ctx, func(context.Context) (*corev1.Pod, error) {
			select {
			case <-started:
			default:
				close(started)
			}
			return &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "shell", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}}}}}, nil
		})
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestPortForwardStartupFailurePreservesInlineFragmentAndStatus(t *testing.T) {
	var requestedPath string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		http.Error(w, "upgrade rejected", http.StatusInternalServerError)
	}))
	defer api.Close()

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
`, api.URL)
	configPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(k8s.NewManager(configPath))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	req := httptest.NewRequest(http.MethodPost, "/portforward", strings.NewReader("localPort=18080&remotePort=80"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("ctx", encodeCtx("test"))
	req.SetPathValue("ns", "default")
	req.SetPathValue("name", "demo")
	rec := httptest.NewRecorder()
	server.handlePortForwardStart(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if !strings.Contains(rec.Body.String(), `class="error-state"`) {
		t.Fatalf("missing inline error fragment: %s", rec.Body.String())
	}
	if requestedPath != "/api/v1/namespaces/default/pods/demo/portforward" {
		t.Fatalf("port-forward request path = %q", requestedPath)
	}
}
func TestWatchRouteSetsHeadersAndSendsInitialRefresh(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/watch", nil).WithContext(ctx)
	req.SetPathValue("resource", "all")
	rec := httptest.NewRecorder()
	(&Server{}).handleWatch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("cache control = %q, want no-cache", got)
	}
	if !strings.Contains(rec.Body.String(), "event: message\ndata: changed\n\n") {
		t.Fatalf("missing initial refresh event: %q", rec.Body.String())
	}
}

func TestWatchStreamErrorAndUpstreamClosureTerminate(t *testing.T) {
	events := make(chan k8s.RowEvent, 1)
	events <- k8s.RowEvent{Error: errors.New("private upstream detail")}
	close(events)
	rec := httptest.NewRecorder()
	server := &Server{}
	server.streamEvents(rec, rec, context.Background(), events)
	body := rec.Body.String()
	if !strings.Contains(body, "event: message\ndata: changed\n\n") ||
		!strings.Contains(body, "event: watch-error\ndata: Live updates stopped after the API server rejected the resource watch.\n\n") ||
		strings.Contains(body, "private upstream detail") {
		t.Fatalf("unexpected SSE body: %q", body)
	}
}
func TestStatusCountsOnlySelectedNamespaceAtHTTPBoundary(t *testing.T) {
	var requestedPath string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"apiVersion":"v1","kind":"PodList","items":[{"metadata":{"name":"run"},"status":{"phase":"Running"}},{"metadata":{"name":"pending"},"status":{"phase":"Pending"}},{"metadata":{"name":"failed"},"status":{"phase":"Failed"}}]}`)
	}))
	defer api.Close()
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
`, api.URL)
	configPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := New(k8s.NewManager(configPath))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/c/test/status?ns=selected", nil)
	req.SetPathValue("ctx", encodeCtx("test"))
	rec := httptest.NewRecorder()
	server.handleStatus(rec, req)
	if requestedPath != "/api/v1/namespaces/selected/pods" {
		t.Fatalf("pods API path = %q, want selected namespace", requestedPath)
	}
	if got := rec.Body.String(); !strings.Contains(got, "1 running") || !strings.Contains(got, "1 failing") {
		t.Fatalf("status body = %q, want deterministic running/failing counts", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("content type = %q, want explicit text/html", got)
	}
}
func TestReadinessReportsMissingAndRemovedContexts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		config   string
		selected string
	}{
		{name: "unreadable kubeconfig", config: filepath.Join(t.TempDir(), "missing")},
		{name: "removed selected context", config: func() string {
			path := filepath.Join(t.TempDir(), "config")
			contents := `apiVersion: v1
kind: Config
contexts:
- name: current
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
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		}(), selected: "removed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, err := New(k8s.NewManager(tc.config))
			if err != nil {
				t.Fatal(err)
			}
			server.SetDefaults(tc.selected, "")
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
			}
			if rec.Body.Len() == 0 {
				t.Fatal("readiness failure body is empty")
			}
		})
	}
}

func TestHealthAndReadinessMethods(t *testing.T) {
	server, err := New(k8s.NewManager(filepath.Join(t.TempDir(), "missing")))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s status = %d, want %d", path, rec.Code, http.StatusMethodNotAllowed)
		}
	}
	health := httptest.NewRecorder()
	server.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Body.String() != "ok\n" {
		t.Fatalf("health response = %d %q, want 200 ok", health.Code, health.Body.String())
	}
}
func TestLogSetupFailurePrecedesWebSocketUpgrade(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	req.SetPathValue("ctx", "invalid-base64!")
	req.SetPathValue("ns", "selected")
	req.SetPathValue("name", "pod")
	rec := httptest.NewRecorder()
	(&Server{}).handleLogs(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if rec.Header().Get("Upgrade") != "" || strings.Contains(rec.Body.String(), "101 Switching Protocols") {
		t.Fatalf("setup error upgraded the connection: headers=%v body=%q", rec.Header(), rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "log stream setup failed") {
		t.Fatalf("unexpected client-facing setup error: %q", rec.Body.String())
	}
}
func TestWatchStreamEndsWhenUpstreamCloses(t *testing.T) {
	events := make(chan k8s.RowEvent)
	close(events)
	rec := httptest.NewRecorder()
	server := &Server{}
	server.streamEvents(rec, rec, context.Background(), events)
	if got := rec.Body.String(); got != "event: message\ndata: changed\n\n" {
		t.Fatalf("closed upstream stream body = %q, want only initial refresh", got)
	}
}
