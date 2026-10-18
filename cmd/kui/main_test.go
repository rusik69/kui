package main

import (
	"os"
	"path/filepath"
	"testing"

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

func TestSelectContextRejectsUnreadableKubeconfig(t *testing.T) {
	if _, err := selectContext(k8s.NewManager(filepath.Join(t.TempDir(), "missing")), ""); err == nil {
		t.Fatal("selectContext() accepted a missing kubeconfig")
	}
}
