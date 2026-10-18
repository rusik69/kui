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

func TestWatchSetupFailureReturnsHTTPErrorBeforeSSECommit(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/watch", nil)
	req.SetPathValue("ctx", "invalid-base64!")
	req.SetPathValue("ns", "default")
	req.SetPathValue("resource", "pods")
	recorder := httptest.NewRecorder()

	(&Server{}).handleWatch(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if got := recorder.Header().Get("Content-Type"); got == "text/event-stream" {
		t.Fatalf("setup failure committed an SSE response: content-type %q", got)
	}
}

func TestWatchSetupErrorWithCRLFIsNotFramedAsSSE(t *testing.T) {
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"upstream failed\r\ndata: injected","reason":"InternalError","code":500}`)
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
	req := httptest.NewRequest(http.MethodGet, "/watch", nil)
	req.SetPathValue("ctx", encodeCtx("test"))
	req.SetPathValue("ns", "default")
	req.SetPathValue("resource", "pods")
	recorder := httptest.NewRecorder()

	server.handleWatch(recorder, req)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusBadGateway, recorder.Body.String())
	}
	if strings.Contains(recorder.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("setup error committed SSE content type %q", recorder.Header().Get("Content-Type"))
	}
	if strings.HasPrefix(recorder.Body.String(), "event:") {
		t.Fatalf("upstream error was emitted as an SSE event: %q", recorder.Body.String())
	}
}
