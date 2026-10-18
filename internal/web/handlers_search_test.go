package web

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
func TestParseSearchQueryQualifiersAndFreeText(t *testing.T) {
	got, err := parseSearchQuery(`kind:"Pod" namespace:"team a" name:web frontend`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != "Pod" || got.Namespace != "team a" || got.Name != "web" || got.Text != "frontend" {
		t.Fatalf("unexpected parsed search query: %+v", got)
	}
}

func TestParseSearchQueryRejectsMalformedQualifiers(t *testing.T) {
	for _, query := range []string{`kind:`, `name:"unterminated`, `namespace:"team"x`, `kind:"   "`} {
		if _, err := parseSearchQuery(query); err == nil {
			t.Errorf("parseSearchQuery(%q) accepted malformed qualifier", query)
		}
	}
}
func TestSearchQualifiersValidateAndMatchWithinNamespace(t *testing.T) {
	for _, query := range []string{`label:"=x"`, `label:"app in ("`, `field:"metadata.name in ("`, `status:broken`, `name:=`} {
		if _, err := parseSearchQuery(query); err == nil {
			t.Errorf("accepted invalid query %q", query)
		}
	}
	if _, err := parseSearchQuery(`label:app=`); err != nil {
		t.Fatalf("empty label value is valid Kubernetes selector syntax: %v", err)
	}
	if _, err := parseSearchQuery(`field:metadata.name==`); err != nil {
		t.Fatalf("empty field value is valid Kubernetes selector syntax: %v", err)
	}
	parsed, err := parseSearchQuery(`label:app=web field:metadata.name=web name:=web status:Running`)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.ExactName || parsed.Name != "web" || parsed.Status != "running" {
		t.Fatalf("parsed qualifiers: %+v", parsed)
	}
	podsRK, _ := k8s.ByName("pods")
	makePod := func(name, ns, label, phase string) unstructured.Unstructured {
		obj := namedObject("Pod", name, ns)
		obj.SetLabels(map[string]string{"app": label})
		obj.Object["status"] = map[string]any{"phase": phase}
		return obj
	}
	results := []k8s.KindResult{{Kind: podsRK, List: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
		makePod("web", "team-a", "web", "Running"), makePod("web-extra", "team-a", "web", "Running"),
		makePod("web", "team-b", "web", "Running"), makePod("web", "team-a", "other", "Running"),
	}}}}
	got, count := searchObjectsFilteredScoped(results, "ctx", "team-a", parsed)
	if count != 1 || len(got) != 1 || got[0].Name != "web" || got[0].ListURL != "/c/ctx/team-a/pods" {
		t.Fatalf("scoped exact selector returned %+v count=%d", got, count)
	}
}

func TestSearchObjectsFiltersAndCountsBeyondCaps(t *testing.T) {
	podsRK, _ := k8s.ByName("pods")
	results := []k8s.KindResult{{
		Kind: podsRK,
		List: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			namedObject("Pod", "web-one", "team a"),
			namedObject("Pod", "web-two", "team a"),
			namedObject("Pod", "web-other", "default"),
		}},
	}}
	got, count := searchObjectsFiltered(results, "myctx", searchQualifier{Kind: "pod", Namespace: "team a", Name: "web"})
	if count != 2 || len(got) != 2 {
		t.Fatalf("qualified search returned %d results, count %d; want 2", len(got), count)
	}
	if got[0].ListURL != "/c/myctx/team%20a/pods" {
		t.Fatalf("result destination lost namespace scope or safe path escaping: %q", got[0].ListURL)
	}
}

func BenchmarkSearchObjectsFiltered10kPods(b *testing.B) {
	podsRK, _ := k8s.ByName("pods")
	items := make([]unstructured.Unstructured, 10_000)
	for i := range items {
		name := fmt.Sprintf("pod-%05d", i)
		if i%2 == 0 {
			name = "match-" + name
		}
		items[i] = namedObject("Pod", name, "benchmark")
	}
	results := []k8s.KindResult{{
		Kind: podsRK,
		List: &unstructured.UnstructuredList{Items: items},
	}}
	query := searchQualifier{Name: "match"}
	const wantCount = 5_000
	wantVisible := min(wantCount, searchObjectCap, searchObjectPerKindCap)
	got, count := searchObjectsFiltered(results, "benchmark", query)
	if count != wantCount || len(got) != wantVisible {
		b.Fatalf("search returned %d results, count %d; want %d results, count %d", len(got), count, wantVisible, wantCount)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		got, count = searchObjectsFiltered(results, "benchmark", query)
		if count != wantCount || len(got) != wantVisible {
			b.Fatalf("search returned %d results, count %d; want %d results, count %d", len(got), count, wantVisible, wantCount)
		}
	}
}

func TestSearchEmptyAndPartialStatesHaveRecoveryText(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	empty := httptest.NewRecorder()
	server.render(empty, "searchresults", searchResults{})
	if !strings.Contains(empty.Body.String(), "Type a name or qualifier") || !strings.Contains(empty.Body.String(), "Shortcuts") {
		t.Fatalf("empty search state lacks search guidance: %s", empty.Body.String())
	}
	partialResults := searchResults{Query: `kind:Pod name:"web app"`, KindFilter: "Pod", Context: "prod", Namespace: "team-a", Error: "Some resource kinds could not be searched: Pods. Retry the search or narrow it with kind:."}
	partialRecorder := httptest.NewRecorder()
	server.render(partialRecorder, "searchresults", partialResults)
	body := partialRecorder.Body.String()
	if !strings.Contains(body, "Retry the search") || !strings.Contains(body, `data-search-query="kind:Pod name:&#34;web app&#34;"`) || !strings.Contains(body, `data-kind-filter="Pod"`) {
		t.Fatalf("partial search state lacks recovery or navigation state: %s", body)
	}
	invalidRecorder := httptest.NewRecorder()
	server.render(invalidRecorder, "searchresults", searchResults{Query: "kind:", InvalidQuery: true, Error: "Search syntax: kind: needs a non-empty value"})
	if !strings.Contains(invalidRecorder.Body.String(), "Search syntax") || !strings.Contains(invalidRecorder.Body.String(), `kind:Pod`) {
		t.Fatalf("malformed query does not explain its syntax recovery: %s", invalidRecorder.Body.String())
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
