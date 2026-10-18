package k8s

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"
)

type preferredResourcesDiscovery struct {
	discovery.DiscoveryInterface
	lists []*metav1.APIResourceList
	err   error
}

func (d preferredResourcesDiscovery) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	return d.lists, d.err
}

func TestAllAPIResourcesFiltersAndSortsWithPartialError(t *testing.T) {
	partialErr := errors.New("one API group is unavailable")
	resources := preferredResourcesDiscovery{err: partialErr, lists: []*metav1.APIResourceList{
		{GroupVersion: "z.example/v1", APIResources: []metav1.APIResource{
			{Name: "widgets", Kind: "Widget", Verbs: metav1.Verbs{"get", "list", "watch"}},
			{Name: "widgets/status", Kind: "Widget", Verbs: metav1.Verbs{"get", "list", "watch"}},
			{Name: "metrics", Kind: "Metric", Verbs: metav1.Verbs{"list", "watch"}},
			{Name: "secrets", Kind: "Secret", Verbs: metav1.Verbs{"get"}},
		}},
		{GroupVersion: "z.example/v2", APIResources: []metav1.APIResource{
			{Name: "widgets", Kind: "Widget", Verbs: metav1.Verbs{"get", "list", "watch"}},
		}},
		{GroupVersion: "a.example/v1", APIResources: []metav1.APIResource{
			{Name: "widgets", Kind: "Widget", Verbs: metav1.Verbs{"get", "list", "watch"}},
		}},
	}}
	got, err := AllAPIResources(&Clients{Discovery: resources})
	if !errors.Is(err, partialErr) {
		t.Fatalf("AllAPIResources() error = %v, want partial discovery error", err)
	}
	if len(got) != 2 || got[0].GVR.Group != "a.example" || got[1].GVR.Group != "z.example" {
		t.Fatalf("AllAPIResources() = %+v, want both grouped resources in deterministic order", got)
	}
	if got[0].Name != "widgets.a.example" || got[1].Name != "widgets.z.example" || got[1].GVR.Version != "v1" {
		t.Fatalf("discovery identities = %+v, want group-qualified names and deterministic v1 selection", got)
	}
}

func TestWatchForwardsSelectorsResourceVersionAndErrorEvents(t *testing.T) {
	pods, _ := ByName("pods")
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	fakeWatch := watch.NewFake()
	var gotFields fields.Selector
	var gotLabels labels.Selector
	client.PrependWatchReactor("pods", func(action ktesting.Action) (bool, watch.Interface, error) {
		restrictions := action.(ktesting.WatchAction).GetWatchRestrictions()
		gotFields, gotLabels = restrictions.Fields, restrictions.Labels
		return true, fakeWatch, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := Watch(ctx, &Clients{Dynamic: client}, pods, "default", metav1.ListOptions{
		ResourceVersion: "42", FieldSelector: "metadata.name=demo", LabelSelector: "app=web",
	})
	if err != nil {
		t.Fatal(err)
	}
	fakeWatch.Error(&metav1.Status{Reason: metav1.StatusReasonExpired, Message: "resource version expired"})
	select {
	case event := <-events:
		if event.Type != watch.Error || event.Object != nil || event.Error == nil {
			t.Fatalf("watch error event = %+v, want preserved error without resource object", event)
		}
	case <-ctx.Done():
		t.Fatal("watch context cancelled before error event")
	}
	if gotFields.String() != "metadata.name=demo" || gotLabels.String() != "app=web" {
		t.Fatalf("watch selectors = (%q, %q), want (metadata.name=demo, app=web)", gotFields.String(), gotLabels.String())
	}
}
