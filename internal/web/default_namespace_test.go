package web

import (
	"testing"

	"github.com/rusik69/kui/internal/k8s"
)

func TestDefaultNamespaceUsesSelectedKubeconfigContext(t *testing.T) {
	contexts := []k8s.ContextInfo{
		{Name: "dev", Namespace: "development"},
		{Name: "prod", Namespace: "production"},
	}
	if got := defaultNamespace(contexts, "prod"); got != "production" {
		t.Fatalf("defaultNamespace(prod) = %q, want production", got)
	}
	if got := defaultNamespace(contexts, "missing"); got != "_all" {
		t.Fatalf("defaultNamespace(missing) = %q, want _all", got)
	}
}

func TestDefaultNamespaceKeepsAllNamespacesWhenUnconfigured(t *testing.T) {
	contexts := []k8s.ContextInfo{{Name: "dev"}}
	if got := defaultNamespace(contexts, "dev"); got != "_all" {
		t.Fatalf("defaultNamespace(dev) = %q, want _all", got)
	}
}
