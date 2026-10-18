package k8s

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

// PodMetrics returns per-pod usage. Missing metrics-server APIs are reported as unavailable;
// other request failures are returned for callers to surface.
func PodMetrics(ctx context.Context, c *Clients, namespace string) (*metricsv1beta1.PodMetricsList, bool, error) {
	if c.Metrics == nil {
		return nil, false, nil
	}
	var (
		list *metricsv1beta1.PodMetricsList
		err  error
	)
	if namespace != "" && namespace != "_all" {
		list, err = c.Metrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	} else {
		list, err = c.Metrics.MetricsV1beta1().PodMetricses(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	}
	if err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return list, true, nil
}

// NodeMetrics returns per-node usage. Missing metrics-server APIs are reported as unavailable;
// other request failures are returned for callers to surface.
func NodeMetrics(ctx context.Context, c *Clients) (*metricsv1beta1.NodeMetricsList, bool, error) {
	if c.Metrics == nil {
		return nil, false, nil
	}
	list, err := c.Metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return list, true, nil
}
