package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDetailYAMLEditorRendersSavedStateControls(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.render(recorder, "detail", DetailData{YAML: "apiVersion: v1\nkind: Pod\n", Name: "demo"})
	body := recorder.Body.String()
	for _, want := range []string{
		`id="yaml-editor"`,
		`id="yaml-dirty-status" class="text-dim" aria-live="polite">Saved</span>`,
		`type="submit" disabled`,
		`>Copy YAML</button>`,
		`data-filename="demo.yaml"`,
		`>Download YAML</button>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("detail HTML is missing YAML dirty-state content %q", want)
		}
	}
}

func TestPodDetailRendersLogAndTerminalControls(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.render(recorder, "detail", DetailData{
		Name:           "demo",
		Kind:           "Pod",
		IsPod:          true,
		ContainerNames: []string{"app"},
		LogsWSURLBase:  "/logs",
		ExecWSURLBase:  "/exec",
	})
	body := recorder.Body.String()
	for _, want := range []string{
		`id="log-tail-lines"`,
		`id="log-pause"`,
		`id="log-filter"`,
		`id="log-status"`,
		`id="shell-status"`,
		`Copy selection`,
		`Clear terminal`,
		`aria-label="Delete Pod demo"`,
		`aria-label="Evict Pod demo"`,
		`aria-label="Close details"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pod detail HTML is missing stream control %q", want)
		}
	}
}
