package web

import (
	"bufio"
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
)

func TestAccessLogRecordsDefaultAndExplicitStatus(t *testing.T) {
	for _, tc := range []struct {
		name        string
		writeHeader bool
		status      int
	}{
		{name: "implicit success", status: http.StatusOK},
		{name: "explicit status", writeHeader: true, status: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			handler := WithAccessLog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.writeHeader {
					w.WriteHeader(tc.status)
				} else {
					_, _ = w.Write([]byte("ok"))
				}
			}), log.New(&logs, "", 0))
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/status", nil))
			if r.Code != tc.status {
				t.Fatalf("response status = %d, want %d", r.Code, tc.status)
			}
			if !strings.Contains(logs.String(), "GET /status "+strconv.Itoa(tc.status)+" ") {
				t.Fatalf("access log does not record status %d: %q", tc.status, logs.String())
			}
		})
	}
}

func TestSSERouteFlushesThroughAccessLogWithoutLoggingQuery(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	s.SetDefaults("offline", "")

	var logs bytes.Buffer
	ts := httptest.NewServer(WithAccessLog(s.Handler(), log.New(&logs, "", 0)))
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/c/offline/_all/overview/watch?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		cancel()
		ts.Close()
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream; charset=utf-8" {
		cancel()
		resp.Body.Close()
		ts.Close()
		t.Fatalf("SSE response = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(resp.Body)
	for _, want := range []string{"event: message", "data: changed", ""} {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			cancel()
			resp.Body.Close()
			ts.Close()
			t.Fatalf("reading initial SSE event: %v", readErr)
		}
		if strings.TrimSpace(line) != want {
			cancel()
			resp.Body.Close()
			ts.Close()
			t.Fatalf("SSE line = %q, want %q", strings.TrimSpace(line), want)
		}
	}
	cancel()
	resp.Body.Close()
	ts.Close()

	got := logs.String()
	if !strings.Contains(got, "GET /c/offline/_all/overview/watch 200 ") {
		t.Fatalf("access log did not record the SSE request: %q", got)
	}
	if strings.Contains(got, "token") || strings.Contains(got, "secret") || strings.Contains(got, "?") {
		t.Fatalf("access log contains query data: %q", got)
	}
}

func TestAccessLogEscapesPathAndOmitsQuery(t *testing.T) {
	var logs bytes.Buffer
	handler := WithAccessLog(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), log.New(&logs, "", 0))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/objects/a%2Fb?token=secret&private=value", nil))
	got := logs.String()
	if !strings.Contains(got, "GET /objects/a%2Fb 200 ") {
		t.Fatalf("access log did not preserve escaped request path: %q", got)
	}
	for _, secret := range []string{"token", "secret", "private", "value", "?"} {
		if strings.Contains(got, secret) {
			t.Fatalf("access log contains query data %q: %q", secret, got)
		}
	}
}

func TestReadinessDoesNotProbeUnreachableAPIServer(t *testing.T) {
	config := `apiVersion: v1
kind: Config
contexts:
- name: offline
  context:
    cluster: unreachable
    user: local
clusters:
- name: unreachable
  cluster:
    server: https://127.0.0.1:1
users:
- name: local
  user: {}
`
	configPath := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(k8s.NewManager(configPath))
	if err != nil {
		t.Fatal(err)
	}
	s.SetDefaults("offline", "")
	r := httptest.NewRecorder()
	s.Handler().ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if r.Code != http.StatusOK || r.Body.String() != "ready: offline\n" {
		t.Fatalf("readiness response = %d %q, want 200 and selected context", r.Code, r.Body.String())
	}
}
