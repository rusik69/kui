package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	webview "github.com/webview/webview_go"

	"github.com/rusik69/kui/internal/k8s"
)

func TestSelectContext(t *testing.T) {
	for _, tc := range []struct {
		name, current, override, want string
		wantErr                       bool
	}{
		{name: "explicit override without current", override: "dev"},
		{name: "missing override", override: "absent", wantErr: true},
		{name: "current context", current: "dev"},
		{
			name: "missing current falls back to first context",
			want: "dev",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := "apiVersion: v1\nkind: Config\n"
			if tc.current != "" {
				config += "current-context: " + tc.current + "\n"
			}
			config += "clusters:\n- name: local\n  cluster:\n    server: https://127.0.0.1:1\ncontexts:\n- name: dev\n  context:\n    cluster: local\n    user: local\nusers:\n- name: local\n  user: {}\n"
			path := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := selectContext(k8s.NewManager(path), tc.override)
			if (err != nil) != tc.wantErr {
				t.Fatalf("selectContext() error = %v, wantErr %v", err, tc.wantErr)
			}
			want := tc.want
			if want == "" && err == nil {
				want = "dev"
			}
			if err == nil && got != want {
				t.Errorf("selectContext() = %q, want %q", got, want)
			}
		})
	}
}

type recordingDesktopWindow struct {
	calls []string

	terminatedOnce sync.Once
	terminated     chan struct{}
	running        chan struct{}
}

func newRecordingDesktopWindow() *recordingDesktopWindow {
	return &recordingDesktopWindow{terminated: make(chan struct{}), running: make(chan struct{})}
}

func (w *recordingDesktopWindow) SetTitle(title string) {
	w.calls = append(w.calls, "title:"+title)
}

func (w *recordingDesktopWindow) SetSize(width, height int, _ webview.Hint) {
	w.calls = append(w.calls, fmt.Sprintf("size:%dx%d", width, height))
}

func (w *recordingDesktopWindow) Navigate(url string) {
	w.calls = append(w.calls, "navigate:"+url)
}

func (w *recordingDesktopWindow) Run() {
	w.calls = append(w.calls, "run")
}

func (w *recordingDesktopWindow) Terminate() {
	w.calls = append(w.calls, "terminate")
}

func (w *recordingDesktopWindow) Destroy() {
	w.calls = append(w.calls, "destroy")
}

func TestOpenDesktopWindowRunsAndDestroysNativeWindow(t *testing.T) {
	window := newRecordingDesktopWindow()
	openDesktopWindow(window, "http://127.0.0.1:4321")
	want := []string{
		"title:kui",
		"size:1280x800",
		"navigate:http://127.0.0.1:4321",
		"run",
		"destroy",
	}
	if got := fmt.Sprint(window.calls); got != fmt.Sprint(want) {
		t.Fatalf("desktop window lifecycle = %s, want %s", got, want)
	}
}

// TestDesktopWindowTerminateUnblocksRun verifies the shutdown contract the
// signal handler relies on: Terminate must unblock a Run that is blocking on
// the platform event loop, so a signal (or a window that never opened) cannot
// leave the app running with no way to close it.
func TestDesktopWindowTerminateUnblocksRun(t *testing.T) {
	window := &blockingDesktopWindow{recordingDesktopWindow: *newRecordingDesktopWindow()}
	done := make(chan struct{})
	go func() {
		openDesktopWindow(window, "http://127.0.0.1:4321")
		close(done)
	}()
	// Wait until Run() is blocked on the event loop, then terminate it the
	// way the signal goroutine does.
	select {
	case <-window.running:
	case <-time.After(2 * time.Second):
		t.Fatal("openDesktopWindow never reached Run")
	}
	window.Terminate()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("openDesktopWindow did not return after Terminate")
	}
	want := []string{"title:kui", "size:1280x800", "navigate:http://127.0.0.1:4321", "run", "terminate", "destroy"}
	if got := fmt.Sprint(window.calls); got != fmt.Sprint(want) {
		t.Fatalf("terminated window lifecycle = %s, want %s", got, want)
	}
}

type blockingDesktopWindow struct {
	recordingDesktopWindow
}

func (w *blockingDesktopWindow) Run() {
	w.calls = append(w.calls, "run")
	close(w.running)
	// Simulate the platform event loop: blocks until Terminate is called.
	<-w.terminated
}

func (w *blockingDesktopWindow) Terminate() {
	w.recordingDesktopWindow.Terminate()
	w.terminatedOnce.Do(func() { close(w.terminated) })
}

func TestSelectContextRejectsUnreadableKubeconfig(t *testing.T) {
	if _, err := selectContext(k8s.NewManager(filepath.Join(t.TempDir(), "missing")), ""); err == nil {
		t.Fatal("selectContext() accepted a missing kubeconfig")
	}
}
