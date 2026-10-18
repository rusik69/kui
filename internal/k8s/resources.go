package k8s

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ResourceKind describes one browsable resource type: its GVR, whether it's
// namespaced, and which sidebar group it belongs to.
type ResourceKind struct {
	Name       string // short name used in URLs, e.g. "pods"
	Group      string // sidebar group, e.g. "Workloads"
	Kind       string // e.g. "Pod"
	GVR        schema.GroupVersionResource
	Namespaced bool
}

// Registry is the static list of resource kinds shown in the sidebar,
// grouped in the order the UI renders them. Adding a kind here is enough to
// make it browsable — list/get/delete/watch all go through the dynamic client.
var Registry = []ResourceKind{
	{Name: "pods", Group: "Workloads", Kind: "Pod", GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true},
	{Name: "deployments", Group: "Workloads", Kind: "Deployment", GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Namespaced: true},
	{Name: "replicasets", Group: "Workloads", Kind: "ReplicaSet", GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, Namespaced: true},
	{Name: "statefulsets", Group: "Workloads", Kind: "StatefulSet", GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, Namespaced: true},
	{Name: "daemonsets", Group: "Workloads", Kind: "DaemonSet", GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, Namespaced: true},
	{Name: "jobs", Group: "Workloads", Kind: "Job", GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, Namespaced: true},
	{Name: "cronjobs", Group: "Workloads", Kind: "CronJob", GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, Namespaced: true},

	{Name: "services", Group: "Network", Kind: "Service", GVR: schema.GroupVersionResource{Version: "v1", Resource: "services"}, Namespaced: true},
	{Name: "ingresses", Group: "Network", Kind: "Ingress", GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, Namespaced: true},
	{Name: "endpoints", Group: "Network", Kind: "Endpoints", GVR: schema.GroupVersionResource{Version: "v1", Resource: "endpoints"}, Namespaced: true},

	{Name: "configmaps", Group: "Config", Kind: "ConfigMap", GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, Namespaced: true},
	{Name: "secrets", Group: "Config", Kind: "Secret", GVR: schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, Namespaced: true},

	{Name: "persistentvolumeclaims", Group: "Storage", Kind: "PersistentVolumeClaim", GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, Namespaced: true},
	{Name: "persistentvolumes", Group: "Storage", Kind: "PersistentVolume", GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, Namespaced: false},
	{Name: "storageclasses", Group: "Storage", Kind: "StorageClass", GVR: schema.GroupVersionResource{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses"}, Namespaced: false},

	{Name: "nodes", Group: "Cluster", Kind: "Node", GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, Namespaced: false},
	{Name: "namespaces", Group: "Cluster", Kind: "Namespace", GVR: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, Namespaced: false},
	{Name: "events", Group: "Cluster", Kind: "Event", GVR: schema.GroupVersionResource{Version: "v1", Resource: "events"}, Namespaced: true},
}

// ByName finds a registry entry by its short URL name.
func ByName(name string) (ResourceKind, bool) {
	for _, r := range Registry {
		if r.Name == name {
			return r, true
		}
	}
	return ResourceKind{}, false
}

// Groups returns the registry partitioned into sidebar groups, in display order.
func Groups() []string {
	seen := map[string]bool{}
	var order []string
	for _, r := range Registry {
		if !seen[r.Group] {
			seen[r.Group] = true
			order = append(order, r.Group)
		}
	}
	return order
}

// InGroup returns every registry entry belonging to the given sidebar group.
func InGroup(group string) []ResourceKind {
	var out []ResourceKind
	for _, r := range Registry {
		if r.Group == group {
			out = append(out, r)
		}
	}
	return out
}

// AllAPIResources walks cluster discovery for the "All resources" page —
// anything the registry doesn't already cover, including CRDs. Partial discovery
// results are returned together with the discovery error.
func AllAPIResources(c *Clients) ([]ResourceKind, error) {
	lists, discoveryErr := c.Discovery.ServerPreferredResources()
	if discoveryErr != nil && len(lists) == 0 {
		return nil, fmt.Errorf("discover resources: %w", discoveryErr)
	}
	type resourceIdentity struct {
		group    string
		resource string
	}
	byIdentity := make(map[resourceIdentity]ResourceKind)
	for _, l := range lists {
		gv, err := schema.ParseGroupVersion(l.GroupVersion)
		if err != nil {
			continue
		}
		for _, r := range l.APIResources {
			if !canBrowse(r.Verbs) || strings.Contains(r.Name, "/") || r.Kind == "" {
				continue
			}
			identity := resourceIdentity{group: gv.Group, resource: r.Name}
			kind := ResourceKind{
				Name: r.Name, Group: "All resources", Kind: r.Kind,
				GVR: gv.WithResource(r.Name), Namespaced: r.Namespaced,
			}
			current, ok := byIdentity[identity]
			if !ok || kind.GVR.Version < current.GVR.Version ||
				(kind.GVR.Version == current.GVR.Version && kind.Kind < current.Kind) {
				byIdentity[identity] = kind
			}
		}
	}
	out := make([]ResourceKind, 0, len(byIdentity))
	for identity, kind := range byIdentity {
		if identity.group != "" {
			kind.Name = identity.resource + "." + identity.group
		}
		out = append(out, kind)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].GVR.Group != out[j].GVR.Group {
			return out[i].GVR.Group < out[j].GVR.Group
		}
		return out[i].GVR.Version < out[j].GVR.Version
	})
	return out, discoveryErr
}

func canBrowse(verbs metav1.Verbs) bool {
	return slices.Contains(verbs, "get") && slices.Contains(verbs, "list") && slices.Contains(verbs, "watch")
}

// List returns every object of the given kind, scoped to namespace unless
// the kind is cluster-scoped or namespace is empty ("" / "_all").
func List(ctx context.Context, c *Clients, rk ResourceKind, namespace string) (*unstructured.UnstructuredList, error) {
	return ListWithOptions(ctx, c, rk, namespace, metav1.ListOptions{})
}

// ListWithOptions lists objects with API-server selectors and pagination options.
func ListWithOptions(ctx context.Context, c *Clients, rk ResourceKind, namespace string, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	return listWith(ctx, c, rk, namespace, opts)
}

// ListFromCache is List served from the API server's watch cache: it passes
// ResourceVersion "0", which declares the client accepts a possibly-stale
// snapshot. A list without a ResourceVersion must be read from etcd for
// consistency, so on a large cluster the cached variant is dramatically
// cheaper. This is right for pollers and keystroke-driven searches (the
// sidebar badges, the 3s status bar, the command palette); the main table
// view deliberately uses List, where freshness is what the user is looking
// at.
func ListFromCache(ctx context.Context, c *Clients, rk ResourceKind, namespace string) (*unstructured.UnstructuredList, error) {
	return listWith(ctx, c, rk, namespace, metav1.ListOptions{ResourceVersion: "0"})
}

func listWith(ctx context.Context, c *Clients, rk ResourceKind, namespace string, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	ri := c.Dynamic.Resource(rk.GVR)
	if rk.Namespaced && namespace != "" && namespace != "_all" {
		return ri.Namespace(namespace).List(ctx, opts)
	}
	return ri.List(ctx, opts)
}

// Get fetches one object by name.
func Get(ctx context.Context, c *Clients, rk ResourceKind, namespace, name string) (*unstructured.Unstructured, error) {
	ri := c.Dynamic.Resource(rk.GVR)
	if rk.Namespaced {
		return ri.Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	}
	return ri.Get(ctx, name, metav1.GetOptions{})
}

// Delete removes one object by name.
func Delete(ctx context.Context, c *Clients, rk ResourceKind, namespace, name string) error {
	ri := c.Dynamic.Resource(rk.GVR)
	if rk.Namespaced {
		return ri.Namespace(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	}
	return ri.Delete(ctx, name, metav1.DeleteOptions{})
}
