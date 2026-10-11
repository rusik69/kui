package web

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/rusik69/kui/internal/k8s"
)

func namedObject(kind, name, namespace string) unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": name, "namespace": namespace},
	}}
}

func TestSearchObjectsSubstringMatch(t *testing.T) {
	podsRK, _ := k8s.ByName("pods")
	results := []k8s.KindResult{{
		Kind: podsRK,
		List: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			namedObject("Pod", "web-server-1", "default"),
			namedObject("Pod", "web-server-2", "default"),
			namedObject("Pod", "database-1", "default"),
		}},
	}}

	got := searchObjects(results, "myctx", "web")

	if len(got) != 2 {
		t.Fatalf("expected 2 matches for \"web\", got %d: %+v", len(got), got)
	}
	for _, o := range got {
		if o.ListURL != "/c/myctx/default/pods" {
			t.Errorf("expected ListURL to point at the resource's list view, got %q", o.ListURL)
		}
	}
}

func TestSearchObjectsSkipsErroredKinds(t *testing.T) {
	podsRK, _ := k8s.ByName("pods")
	results := []k8s.KindResult{{Kind: podsRK, Err: os.ErrPermission}}

	got := searchObjects(results, "myctx", "anything")
	if len(got) != 0 {
		t.Errorf("expected no results from an errored kind, got %+v", got)
	}
}

func TestSearchObjectsPerKindAndTotalCaps(t *testing.T) {
	podsRK, _ := k8s.ByName("pods")
	var items []unstructured.Unstructured
	for i := 0; i < searchObjectPerKindCap+5; i++ {
		items = append(items, namedObject("Pod", "match-pod", "default"))
	}
	results := []k8s.KindResult{{Kind: podsRK, List: &unstructured.UnstructuredList{Items: items}}}

	got := searchObjects(results, "myctx", "match")

	if len(got) != searchObjectPerKindCap {
		t.Errorf("expected per-kind cap of %d, got %d", searchObjectPerKindCap, len(got))
	}
}

func TestSearchCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(testKubeconfigForSearch), 0o600); err != nil {
		t.Fatalf("write test kubeconfig: %v", err)
	}
	s := &Server{mgr: k8s.NewManager(path)}

	cmds := s.searchCommands("ctxid", "_all", "pod")

	foundGoToPod := false
	for _, c := range cmds {
		if c.Label == "Go to Pod" {
			foundGoToPod = true
			if c.FullNav {
				t.Error("\"Go to Pod\" should not be a full navigation")
			}
		}
	}
	if !foundGoToPod {
		t.Errorf("expected \"Go to Pod\" among commands for query \"pod\", got %+v", cmds)
	}

	switchCmds := s.searchCommands("ctxid", "_all", "switch to prod")
	if len(switchCmds) != 1 || switchCmds[0].Label != "Switch to prod" || !switchCmds[0].FullNav {
		t.Errorf("expected exactly one \"Switch to prod\" full-nav command, got %+v", switchCmds)
	}
}

const testKubeconfigForSearch = `
apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: c
  cluster:
    server: https://example.com
contexts:
- name: prod
  context:
    cluster: c
users: []
`
