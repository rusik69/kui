package k8s

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

// PodMetrics returns per-pod usage, or ok=false if metrics-server isn't installed.
func PodMetrics(ctx context.Context, c *Clients, namespace string) (list *metricsv1beta1.PodMetricsList, ok bool) {
	if c.Metrics == nil {
		return nil, false
	}
	var err error
	if namespace != "" && namespace != "_all" {
		list, err = c.Metrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	} else {
		list, err = c.Metrics.MetricsV1beta1().PodMetricses(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, false
	}
	return list, true
}

// NodeMetrics returns per-node usage, or ok=false if metrics-server isn't installed.
func NodeMetrics(ctx context.Context, c *Clients) (list *metricsv1beta1.NodeMetricsList, ok bool) {
	if c.Metrics == nil {
		return nil, false
	}
	list, err := c.Metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, false
	}
	return list, true
}
