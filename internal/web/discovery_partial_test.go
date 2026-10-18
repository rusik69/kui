package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
)

type partialDiscoveryFixture struct {
	discovery.DiscoveryInterface
	resources []*metav1.APIResourceList
	err       error
}

func (d partialDiscoveryFixture) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return d.resources, d.err
}

func TestAllResourceDirectoryRetainsPartialDiscovery(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	partialErr := errors.New("temporarily unavailable <script>")
	clients := &k8s.Clients{Discovery: partialDiscoveryFixture{
		resources: []*metav1.APIResourceList{{
			GroupVersion: "widgets.example.com/v1",
			APIResources: []metav1.APIResource{{
				Name: "widgets", Kind: "Widget", Namespaced: true,
				Verbs: metav1.Verbs{"get", "list", "watch"},
			}},
		}},
		err: partialErr,
	}}

	req := httptest.NewRequest(http.MethodGet, "/c/ctx/_all/all?frame=table", nil)
	rec := httptest.NewRecorder()
	server.renderAllResourcesDirectory(rec, req, "ctx", "_all", clients)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Widget") {
		t.Fatalf("partial resource missing from rendered table: %s", body)
	}
	if !strings.Contains(body, "temporarily unavailable &lt;script&gt;") {
		t.Fatalf("discovery warning missing or not escaped: %s", body)
	}
	if strings.Contains(body, "temporarily unavailable <script>") {
		t.Fatalf("unescaped discovery error in response: %s", body)
	}
}

func TestResolveKindFindsPartiallyDiscoveredResource(t *testing.T) {
	clients := &k8s.Clients{Discovery: partialDiscoveryFixture{
		resources: []*metav1.APIResourceList{{
			GroupVersion: "widgets.example.com/v1",
			APIResources: []metav1.APIResource{{
				Name: "widgets", Kind: "Widget", Namespaced: true,
				Verbs: metav1.Verbs{"get", "list", "watch"},
			}},
		}},
		err: errors.New("one API group unavailable"),
	}}

	got, ok := resolveKind(clients, "widgets.widgets.example.com")
	if !ok || got.GVR.Group != "widgets.example.com" || got.GVR.Resource != "widgets" {
		t.Fatalf("resolveKind() = %+v, %v; want partially discovered Widget, true", got, ok)
	}
}

func TestAllResourceDirectoryFailsWhenDiscoveryReturnsNoResources(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	clients := &k8s.Clients{Discovery: partialDiscoveryFixture{err: errors.New("discovery unavailable")}}
	req := httptest.NewRequest(http.MethodGet, "/c/ctx/_all/all?frame=table", nil)
	rec := httptest.NewRecorder()
	server.renderAllResourcesDirectory(rec, req, "ctx", "_all", clients)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}
