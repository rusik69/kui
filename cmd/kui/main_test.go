package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	webview "github.com/webview/webview_go"

	"github.com/rusik69/kui/internal/k8s"
)

func TestSelectContext(t *testing.T) {
	for _, tc := range []struct {
		name, current, override string
		wantErr                 bool
	}{
		{name: "missing current", wantErr: true},
		{name: "explicit override without current", override: "dev"},
		{name: "missing override", override: "absent", wantErr: true},
		{name: "current context", current: "dev"},
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
			if err == nil && got != "dev" {
				t.Errorf("selectContext() = %q, want dev", got)
			}
		})
	}
}

type recordingDesktopWindow struct {
	calls []string
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

func (w *recordingDesktopWindow) Destroy() {
	w.calls = append(w.calls, "destroy")
}

func TestOpenDesktopWindowRunsAndDestroysNativeWindow(t *testing.T) {
	window := &recordingDesktopWindow{}
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

func TestSelectContextRejectsUnreadableKubeconfig(t *testing.T) {
	if _, err := selectContext(k8s.NewManager(filepath.Join(t.TempDir(), "missing")), ""); err == nil {
		t.Fatal("selectContext() accepted a missing kubeconfig")
	}
}
