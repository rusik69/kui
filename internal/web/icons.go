package web

// GroupIcon maps a sidebar group name to an icon symbol id (see
// templates/partials/icons.html). Falls back to a generic symbol.
func GroupIcon(group string) string {
	switch group {
	case "Workloads":
		return "icon-workloads"
	case "Network":
		return "icon-network"
	case "Config":
		return "icon-config"
	case "Storage":
		return "icon-storage"
	case "Cluster":
		return "icon-cluster"
	default:
		return "icon-all"
	}
}
