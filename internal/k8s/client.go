package k8s

import (
	"fmt"

	"k8s.io/client-go/discovery"
	discocached "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Clients bundles every client needed to talk to one cluster/context.
type Clients struct {
	Config    *rest.Config
	Clientset kubernetes.Interface
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
	Mapper    *restmapper.DeferredDiscoveryRESTMapper
	Metrics   metricsclient.Interface // nil if metrics-server isn't reachable
}

// Clients returns the cached client bundle for ctxName, building it on first use.
func (m *Manager) Clients(ctxName string) (*Clients, error) {
	m.refreshIfKubeconfigChanged()
	m.mu.RLock()
	c, ok := m.clients[ctxName]
	m.mu.RUnlock()
	if ok {
		return c, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clients[ctxName]; ok {
		return c, nil
	}

	overrides := &clientcmd.ConfigOverrides{CurrentContext: ctxName}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(m.loadingRules, overrides).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load context %q: %w", ctxName, err)
	}
	// client-go's library default (QPS=5, Burst=10) is tuned for well-behaved
	// controllers sharing an API server with many other clients — far too
	// low for a local, single-user interactive tool that legitimately fans
	// out ~17 concurrent List calls on page load, every counts-poll tick,
	// and every command-palette keystroke. kubectl itself raises these
	// (QPS=50, Burst=100); match that baseline here for the same reason.
	cfg.QPS = 50
	cfg.Burst = 100

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build clientset for %q: %w", ctxName, err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client for %q: %w", ctxName, err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("build discovery client for %q: %w", ctxName, err)
	}
	cachedDisco := discocached.NewMemCacheClient(disco)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDisco)

	// Metrics API is optional (requires metrics-server); build the client but
	// don't fail cluster setup if it's absent — callers check availability
	// by calling the metrics API and treating NotFound/discovery errors as "unavailable".
	metrics, _ := metricsclient.NewForConfig(cfg)

	c = &Clients{
		Config:    cfg,
		Clientset: clientset,
		Dynamic:   dyn,
		Discovery: cachedDisco,
		Mapper:    mapper,
		Metrics:   metrics,
	}
	m.clients[ctxName] = c
	return c, nil
}
