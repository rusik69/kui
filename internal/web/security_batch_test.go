package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestProtectMutationRequiresSameOriginAndToken(t *testing.T) {
	called := false
	handler := (&Server{csrfToken: "secret"}).protectMutation(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	tests := []struct {
		name       string
		origin     string
		token      string
		wantStatus int
	}{
		{name: "valid", origin: "http://kui.example", token: "secret", wantStatus: http.StatusNoContent},
		{name: "cross-origin", origin: "http://attacker.example", token: "secret", wantStatus: http.StatusForbidden},
		{name: "missing origin", token: "secret", wantStatus: http.StatusForbidden},
		{name: "missing token", origin: "http://kui.example", wantStatus: http.StatusForbidden},
		{name: "invalid token", origin: "http://kui.example", token: "other", wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodPost, "http://kui.example/action", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.token != "" {
				req.Header.Set("X-Kui-CSRF", tt.token)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			if called != (tt.wantStatus == http.StatusNoContent) {
				t.Fatalf("handler called = %v for status %d", called, recorder.Code)
			}
		})
	}
}

func TestSameOriginRequestRejectsMalformedOrigins(t *testing.T) {
	for _, origin := range []string{
		"https://kui.example",
		"http://kui.example.evil/",
		"http://user@kui.example",
		"http://kui.example/path",
		"http://kui.example?query",
		"null",
	} {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "http://kui.example/action", nil)
			req.Header.Set("Origin", origin)
			if sameOriginRequest(req) {
				t.Errorf("sameOriginRequest accepted %q", origin)
			}
		})
	}
}

func TestWSReaderRejectsOversizedExecMessage(t *testing.T) {
	readResult := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			readResult <- err
			return
		}
		defer conn.Close()
		_, err = newWSReader(conn).Read(make([]byte, 1))
		readResult <- err
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, make([]byte, maxExecMessageBytes+1)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readResult:
		if err == nil {
			t.Fatal("oversized exec message was accepted")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not reject oversized exec message")
	}
}

func TestWSWriterSendsTextMessage(t *testing.T) {
	message := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, data, err := conn.ReadMessage()
		if err == nil {
			message <- string(data)
		}
	}))
	defer server.Close()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if n, err := (&wsWriter{conn: conn}).Write([]byte("terminal output")); err != nil || n != len("terminal output") {
		t.Fatalf("wsWriter.Write() = %d, %v", n, err)
	}
	select {
	case got := <-message:
		if got != "terminal output" {
			t.Fatalf("message = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive WebSocket message")
	}
}
