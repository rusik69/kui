package k8s

import (
	"context"

	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Evict requests graceful pod eviction (respects PodDisruptionBudgets),
// distinct from a hard Delete.
func Evict(ctx context.Context, c *Clients, namespace, name string) error {
	return c.Clientset.PolicyV1().Evictions(namespace).Evict(ctx, &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	})
}
