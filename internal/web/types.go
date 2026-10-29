package web

import (
	"time"

	"github.com/rusik69/kui/internal/k8s"
)

// ContextOption is one entry in the context switcher: ID is the
// base64url-encoded value used in URLs, Name is the real kubeconfig context
// name shown to the user (see ctxid.go for why these must differ).
type ContextOption struct {
	ID, Name string
	Current  bool
}

// PageData backs the full-page shell ("page") and the content fragment
// ("content") — sidebar, topbar selects, and the initial list/watch URLs.
type PageData struct {
	Context       string // encoded context id, used to build every URL below
	ContextName   string // real context name, display only
	Contexts      []ContextOption
	Namespace     string
	Namespaces    []string
	Resource      string
	Groups        []string
	GroupKinds    map[string][]k8s.ResourceKind
	ListURL       string
	WatchURL      string
	BulkDeleteURL string
	CSRFToken     string
	ConnError     string // non-empty when the cluster was unreachable while building this page; shell still renders

	// RancherConfigured reflects whether a Rancher server is currently set
	// up (via flags or the settings dialog); it only changes the token
	// placeholder shown in the settings dialog.
	RancherConfigured bool
}

// TableData backs the "table" fragment.
type TableData struct {
	Kind              string
	Columns           []string
	Rows              []Row
	DiscoveryWarning  string // partial API discovery warning for the "All resources" table
	HeaderColumnCount int
	ShowNamespace     bool
	MetricsURL        string // set for pods/nodes when metrics-server may be available
	RowsAreKinds      bool   // true for the "All resources" directory: rows navigate like sidebar links, not detail rows
	Selectable        bool   // true when rows support bulk-select checkboxes (namespaced/cluster objects, not the kinds directory)
	HasMetrics        bool   // true for pods/nodes: rows get a data-metric-key for client-side CPU/Mem patching
	LabelSelector     string
	FieldSelector     string
	PageSize          int64
	ResourceVersion   string
	NextContinue      string
	NextPageURL       string
}

// Condition is one status condition shown in the Details tab.
type Condition struct {
	Type, Status, Reason, Message string
}

// Container is one container row shown in the Details tab.
type Container struct {
	Name, Image, State string
	Ready              bool
	Restarts           int64
}

// Event is one event row shown in the Events tab.
type Event struct {
	Type, Reason, Message, Age string
}

// DetailData backs the "detail" fragment for a single object.
type DetailData struct {
	Context, Namespace, Resource, Name, Kind string
	Age                                      string
	DotClass                                 string
	UID, APIVersion, ResourceVersion         string
	CanonicalURL                             string
	Labels, Annotations                      map[string]string
	Owners                                   []OwnerReference
	Conditions                               []Condition
	Containers                               []Container
	Events                                   []Event
	YAML                                     string
	YAMLDiffURL                              string
	DeleteURL                                string

	IsPod             bool
	EvictURL          string
	ContainerNames    []string
	LogsWSURLBase     string
	ExecWSURLBase     string
	PortForwardURL    string
	DefaultRemotePort int32

	IsService bool

	IsDeployment bool
	HistoryURL   string

	IsNode          bool
	Addresses       []NodeAddress
	NodeInfo        NodeInfo
	Taints          []NodeTaint
	CPUAllocatable  string
	MemAllocatable  string
	PodsAllocatable string
	PodsOnNode      []PodOnNode

	IsSecret   bool
	SecretData []SecretEntry
}

type OwnerReference struct {
	Kind, Name, DetailURL string
}

// NodeAddress is one entry from a Node's status.addresses.
type NodeAddress struct{ Type, Address string }

// NodeInfo is the subset of a Node's status.nodeInfo shown in its detail pane.
type NodeInfo struct{ OS, Arch, Kernel, Runtime string }

// NodeTaint is one entry from a Node's spec.taints.
type NodeTaint struct{ Key, Value, Effect string }

// PodOnNode is one row in a Node detail's "Pods on this node" table.
type PodOnNode struct {
	Name, Namespace, Status string
	DetailURL               string
}

// SecretEntry is one key in a Secret's data map — Value is the raw base64
// string as stored; decoding happens client-side only on explicit reveal.
type SecretEntry struct{ Key, Value string }

// YAMLDiffData backs the "yamldiff" fragment: a diff preview shown before
// the real apply.
type YAMLDiffData struct {
	Lines    []DiffLine
	Changed  bool
	ApplyURL string
	YAML     string // the edited text, carried through to the confirm-apply form
}

// DebugShellData backs the "debugshell" fragment.
type DebugShellData struct {
	Pod, Namespace, ExecWSURLBase string
	NodeName                      string // non-empty for a shell attached to a node
	KillURL                       string // POST target for the kill button; empties the detail pane
}

// AttentionItem is one unhealthy pod shown in Overview's "Needs attention" list.
type AttentionItem struct {
	Name, Namespace, Reason, Detail, Severity string
}

// EventGroup summarizes recent Kubernetes events by type and reason.
type EventGroup struct {
	Type, Reason string
	Count        int
	LatestAge    string
	URL          string
	SearchText   string
	Events       []OverviewEvent
	MoreEvents   int
}

type OverviewEvent struct {
	UID             string `json:"uid"`
	ResourceVersion string `json:"resourceVersion"`
	Namespace       string `json:"namespace"`
	Kind            string `json:"kind"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	Reason          string `json:"reason"`
	Message         string `json:"message"`
	Age             string `json:"age"`
	URL             string `json:"url"`
}

// OverviewData backs the "overview" fragment: cluster-wide summary tiles.
type OverviewData struct {
	Context, Namespace                     string
	UpdatedAt                              time.Time
	Error                                  string
	EventsAvailable                        bool
	EventsError                            string
	EventGroups                            []EventGroup
	MoreEventGroups                        int
	NodeCount, NodeReadyCount              int
	NamespaceCount                         int
	PodRunning, PodPending, PodFailed      int
	WarningCount                           int
	RecentWarnings                         []Event
	CPURequestedMilli, CPUAllocatableMilli int64
	MemRequestedBytes, MemAllocatableBytes int64
	PodsUsed, PodsAllocatable              int64

	NeedsAttention []AttentionItem
	RecentActivity []Event
	ObjectCounts   []ObjectCount
}
