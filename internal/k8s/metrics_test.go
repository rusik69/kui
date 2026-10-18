package k8s

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ktesting "k8s.io/client-go/testing"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

func TestPodMetricsPreservesForbiddenError(t *testing.T) {
	client := metricsfake.NewSimpleClientset()
	client.PrependReactor("list", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Group: "metrics.k8s.io", Resource: "pods"}, "", errors.New("access denied"))
	})

	list, ok, err := PodMetrics(context.Background(), &Clients{Metrics: client}, "default")
	if ok || list != nil || !apierrors.IsForbidden(err) {
		t.Fatalf("PodMetrics() = (%v, %v, %v), want unavailable with forbidden error", list, ok, err)
	}
}
func TestNodeMetricsTreatsMissingAPIAsUnavailable(t *testing.T) {
	client := metricsfake.NewSimpleClientset()
	client.PrependReactor("list", "nodes", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "metrics.k8s.io", Resource: "nodes"}, "")
	})

	list, ok, err := NodeMetrics(context.Background(), &Clients{Metrics: client})
	if ok || list != nil || err != nil {
		t.Fatalf("NodeMetrics() = (%v, %v, %v), want unavailable without error", list, ok, err)
	}
}

func TestPodMetricsWithoutClientIsUnavailable(t *testing.T) {
	list, ok, err := PodMetrics(context.Background(), &Clients{}, metav1.NamespaceAll)
	if ok || list != nil || err != nil {
		t.Fatalf("PodMetrics() = (%v, %v, %v), want unavailable without error", list, ok, err)
	}
}
