package k8s

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ktesting "k8s.io/client-go/testing"
)

func TestListWithOptionsForwardsSelectors(t *testing.T) {
	pod := func(name, app string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: "default", Labels: map[string]string{"app": app},
		}}
	}
	fake := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme,
		pod("web-1", "web"), pod("web-2", "web"), pod("worker-1", "worker"),
	)
	var restrictions ktesting.ListRestrictions
	fake.PrependReactor("list", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		restrictions = action.(ktesting.ListAction).GetListRestrictions()
		return false, nil, nil
	})
	client := &Clients{Dynamic: fake}
	pods, ok := ByName("pods")
	if !ok {
		t.Fatal("pods resource kind is not registered")
	}

	if _, err := ListWithOptions(context.Background(), client, pods, "default", metav1.ListOptions{
		LabelSelector: "app=web",
		FieldSelector: "metadata.name=web-2",
	}); err != nil {
		t.Fatal(err)
	}
	if got := restrictions.Labels.String(); got != "app=web" {
		t.Errorf("label selector sent to dynamic client = %q, want app=web", got)
	}
	if got := restrictions.Fields.String(); got != "metadata.name=web-2" {
		t.Errorf("field selector sent to dynamic client = %q, want metadata.name=web-2", got)
	}
}
