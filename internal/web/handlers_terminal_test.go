package web

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/rusik69/kui/internal/k8s"
)

// terminalTestServer is a Server over a kubeconfig with one context pointing
// at an unreachable API server. The terminal spawns a local shell — no
// cluster needed for the assertions here.
func terminalTestServer(t *testing.T) *Server {
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
	t.Cleanup(server.Close)
	return server
}

func dialTerminal(t *testing.T, server *Server, ctxID string) (*websocket.Conn, *httptest.Server) {
	t.Helper()
	api := httptest.NewServer(server.Handler())
	t.Cleanup(api.Close)
	wsURL := "ws" + strings.TrimPrefix(api.URL, "http") + "/terminal/" + ctxID
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial terminal: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, api
}

// readTerminal collects PTY output until the needle appears, the shell
// closes the connection, or the timeout passes. gorilla marks a connection
// failed after any errored read (deadlines included), so deadline-based
// polling would poison it — reads happen in a goroutine and results flow
// through a channel instead.
func readTerminal(t *testing.T, conn *websocket.Conn, needle string, timeout time.Duration) string {
	t.Helper()
	messages := make(chan []byte, 32)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				close(messages)
				return
			}
			messages <- message
		}
	}()
	var out strings.Builder
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case message, ok := <-messages:
			if !ok {
				return out.String()
			}
			out.Write(message)
			if strings.Contains(out.String(), needle) {
				return out.String()
			}
		case <-timer.C:
			return out.String()
		}
	}
}

func TestTerminalRunsShellWithSnapshotKubeconfig(t *testing.T) {
	server := terminalTestServer(t)
	ctxID := encodeCtx("dev")
	conn, _ := dialTerminal(t, server, ctxID)
	time.Sleep(500 * time.Millisecond) // let the shell start

	// A deterministic marker proves the shell runs with the snapshot env:
	// ask the shell to print $KUBECONFIG and require a path inside a
	// kui-kubeconfig temp dir.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("echo KUBECONFIG=$KUBECONFIG\r")); err != nil {
		t.Fatalf("send: %v", err)
	}
	out := readTerminal(t, conn, "kui-kubeconfig-", 15*time.Second)
	if !strings.Contains(out, "kui-kubeconfig-") {
		t.Fatalf("terminal output missing KUBECONFIG snapshot path:\n%s", out)
	}
}

func TestTerminalRejectsUnknownContext(t *testing.T) {
	server := terminalTestServer(t)
	unknown := encodeCtx("missing")
	api := httptest.NewServer(server.Handler())
	t.Cleanup(api.Close)
	resp, err := http.Get(api.URL + "/terminal/" + unknown)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown context status = %d, want 502/400", resp.StatusCode)
	}
}

func TestTerminalCloseReapsShellAndSnapshot(t *testing.T) {
	server := terminalTestServer(t)
	ctxID := encodeCtx("dev")

	mgr := server.mgr
	term, err := mgr.NewLocalTerminal("dev", "/bin/sh")
	if err != nil {
		t.Skipf("cannot spawn /bin/sh here: %v", err)
	}
	snapshot := term.SnapshotPath()
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("snapshot kubeconfig missing: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(snapshot); !os.IsNotExist(err) {
		t.Fatalf("snapshot survived Close: %v", err)
	}
	_ = ctxID
	_ = base64.RawURLEncoding
}
