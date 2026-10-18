package web

import (
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Row is one rendered table row: cells aligned to the table's Columns, plus
// enough identity to build the detail-drawer link and a status dot class.
type Row struct {
	Name        string
	Namespace   string
	Cells       []string
	Annotations map[string]string
	CellClasses []string // optional, same length as Cells when set; "" entries mean no extra class
	DotClass    string   // "dot-green" | "dot-yellow" | "dot-red" | "dot-gray" | ""
	DetailURL   string   // filled in by the handler after RowCells returns
}

// Columns returns the table header for a resource kind. "Namespace" is
// prepended by the caller when listing across all namespaces.
func Columns(resourceName string) []string {
	switch resourceName {
	case "pods":
		return []string{"Name", "Ready", "State", "Restarts", "Node", "Age"}
	case "deployments":
		return []string{"Name", "Ready", "Up-to-date", "Available", "Age"}
	case "replicasets":
		return []string{"Name", "Desired", "Current", "Ready", "Age"}
	case "statefulsets", "daemonsets":
		return []string{"Name", "Ready", "Age"}
	case "jobs":
		return []string{"Name", "Completions", "Duration", "Age"}
	case "cronjobs":
		return []string{"Name", "Schedule", "Suspend", "Last Schedule", "Age"}
	case "services":
		return []string{"Name", "Type", "Cluster-IP", "Ports", "Age"}
	case "ingresses":
		return []string{"Name", "Hosts", "Age"}
	case "endpoints":
		return []string{"Name", "Endpoints", "Age"}
	case "configmaps":
		return []string{"Name", "Keys", "Age"}
	case "secrets":
		return []string{"Name", "Type", "Keys", "Age"}
	case "persistentvolumeclaims":
		return []string{"Name", "Status", "Volume", "Capacity", "Age"}
	case "persistentvolumes":
		return []string{"Name", "Capacity", "Status", "Claim", "Age"}
	case "storageclasses":
		return []string{"Name", "Provisioner", "Age"}
	case "nodes":
		return []string{"Name", "Status", "Roles", "Version", "Age"}
	case "namespaces":
		return []string{"Name", "Status", "Age"}
	case "events":
		return []string{"Last Seen", "Type", "Reason", "Object", "Message"}
	default:
		return []string{"Name", "Kind", "Age"}
	}
}

// RowCells extracts the cells for one object, matching Columns(resourceName).
func RowCells(resourceName string, obj *unstructured.Unstructured) Row {
	name := obj.GetName()
	ns := obj.GetNamespace()
	age := age(obj.GetCreationTimestamp().Time)

	switch resourceName {
	case "pods":
		return podRow(obj, name, ns, age)
	case "deployments":
		return deploymentRow(obj, name, ns, age)
	case "replicasets":
		return replicaSetRow(obj, name, ns, age)
	case "statefulsets", "daemonsets":
		return simpleReadyRow(obj, name, ns, age)
	case "jobs":
		return jobRow(obj, name, ns, age)
	case "cronjobs":
		return cronJobRow(obj, name, ns, age)
	case "services":
		return serviceRow(obj, name, ns, age)
	case "ingresses":
		return ingressRow(obj, name, ns, age)
	case "endpoints":
		return endpointsRow(obj, name, ns, age)
	case "configmaps":
		return keyedRow(obj, name, ns, age, "data")
	case "secrets":
		return secretRow(obj, name, ns, age)
	case "persistentvolumeclaims":
		return pvcRow(obj, name, ns, age)
	case "persistentvolumes":
		return pvRow(obj, name, ns, age)
	case "storageclasses":
		provisioner, _, _ := unstructured.NestedString(obj.Object, "provisioner")
		return Row{Name: name, Namespace: ns, Cells: []string{name, provisioner, age}}
	case "nodes":
		return nodeRow(obj, name, ns, age)
	case "namespaces":
		phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
		return Row{Name: name, Namespace: ns, Cells: []string{name, phase, age}, DotClass: dotForPhase(phase)}
	case "events":
		return eventRow(obj, name, ns)
	default:
		kind := obj.GetKind()
		return Row{Name: name, Namespace: ns, Cells: []string{name, kind, age}}
	}
}

// knownBadReasons are real Kubernetes container waiting/terminated reasons
// that don't contain "Error"/"CrashLoop"/"BackOff" and so would otherwise
// slip past podStateIsBad's substring heuristic below — most notably
// OOMKilled (a terminated reason, not even a waiting one) and ErrImagePull.
var knownBadReasons = map[string]bool{
	"ErrImagePull":        true,
	"InvalidImageName":    true,
	"ErrImageNeverPull":   true,
	"RegistryUnavailable": true,
	"ContainerCannotRun":  true,
	"DeadlineExceeded":    true,
	"OOMKilled":           true,
}

// podStateIsBad is the shared "is this pod-state string a real problem"
// predicate — notably, CrashLoopBackOff/ImagePullBackOff pods stay at
// phase=Running in Kubernetes, so checking phase alone misses the single
// most common real-world failure. Used by podRow's dot coloring and by the
// counts/overview "failing" tallies so all three agree on what counts as bad.
func podStateIsBad(state string) bool {
	return knownBadReasons[state] || state == "Failed" ||
		strings.Contains(state, "Error") || strings.Contains(state, "CrashLoop") || strings.Contains(state, "BackOff")
}

// podStateFromUnstructured extracts the same per-pod state string podRow
// computes (phase, overridden by a container's waiting/terminated reason),
// for callers that only need the health check, not a full table row.
func podStateFromUnstructured(obj *unstructured.Unstructured) string {
	state, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	containers, _, _ := unstructured.NestedSlice(obj.Object, "status", "containerStatuses")
	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if waiting, ok, _ := unstructured.NestedMap(cm, "state", "waiting"); ok {
			if reason, _, _ := unstructured.NestedString(waiting, "reason"); reason != "" {
				state = reason
			}
		} else if terminated, ok, _ := unstructured.NestedMap(cm, "state", "terminated"); ok {
			if reason, _, _ := unstructured.NestedString(terminated, "reason"); reason != "" {
				state = reason
			}
		}
	}
	return state
}

func podRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	containers, _, _ := unstructured.NestedSlice(obj.Object, "status", "containerStatuses")
	ready, total, restarts := 0, len(containers), int64(0)
	state := phase
	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if r, ok := cm["ready"].(bool); ok && r {
			ready++
		}
		if rc, ok, _ := unstructured.NestedInt64(cm, "restartCount"); ok {
			restarts += rc
		}
		if waiting, ok, _ := unstructured.NestedMap(cm, "state", "waiting"); ok {
			if reason, _, _ := unstructured.NestedString(waiting, "reason"); reason != "" {
				state = reason
			}
		} else if terminated, ok, _ := unstructured.NestedMap(cm, "state", "terminated"); ok {
			if reason, _, _ := unstructured.NestedString(terminated, "reason"); reason != "" {
				state = reason
			}
		}
	}
	if total == 0 {
		total = 1 // avoid 0/0 before container statuses are populated
	}
	node, _, _ := unstructured.NestedString(obj.Object, "spec", "nodeName")
	dot := "dot-yellow"
	switch {
	case state == "Running" && ready == total:
		dot = "dot-green"
	case state == "Succeeded":
		dot = "dot-gray"
	case podStateIsBad(state):
		dot = "dot-red"
	}
	restartClass := ""
	if restarts > 5 {
		restartClass = "cell-danger"
	} else if restarts > 0 {
		restartClass = "cell-warn"
	}
	return Row{
		Name: name, Namespace: ns, DotClass: dot,
		Cells:       []string{name, fmt.Sprintf("%d/%d", ready, total), state, fmt.Sprintf("%d", restarts), node, age},
		CellClasses: []string{"", "", "", restartClass, "", ""},
	}
}

// readinessDot gives the shared "ready-vs-desired" dot color used by
// Deployments/ReplicaSets/StatefulSets/DaemonSets/Jobs: gray for an
// intentional desired==0 (scale-down), green once ready has caught up,
// yellow otherwise.
func readinessDot(ready, desired int64) string {
	if desired == 0 {
		return "dot-gray"
	}
	if ready == desired {
		return "dot-green"
	}
	return "dot-yellow"
}

func deploymentRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	desired, _, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
	upToDate, _, _ := unstructured.NestedInt64(obj.Object, "status", "updatedReplicas")
	available, _, _ := unstructured.NestedInt64(obj.Object, "status", "availableReplicas")
	return Row{
		Name: name, Namespace: ns, DotClass: readinessDot(ready, desired),
		Cells: []string{name, fmt.Sprintf("%d/%d", ready, desired), fmt.Sprintf("%d", upToDate), fmt.Sprintf("%d", available), age},
	}
}

func replicaSetRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	desired, _, _ := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	current, _, _ := unstructured.NestedInt64(obj.Object, "status", "replicas")
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
	return Row{Name: name, Namespace: ns, DotClass: readinessDot(ready, desired), Cells: []string{name, fmt.Sprintf("%d", desired), fmt.Sprintf("%d", current), fmt.Sprintf("%d", ready), age}}
}

func simpleReadyRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	ready, _, _ := unstructured.NestedInt64(obj.Object, "status", "readyReplicas")
	total, _, _ := unstructured.NestedInt64(obj.Object, "status", "replicas")
	return Row{Name: name, Namespace: ns, DotClass: readinessDot(ready, total), Cells: []string{name, fmt.Sprintf("%d/%d", ready, total), age}}
}

func jobRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	succeeded, _, _ := unstructured.NestedInt64(obj.Object, "status", "succeeded")
	completions, _, _ := unstructured.NestedInt64(obj.Object, "spec", "completions")
	dot := readinessDot(succeeded, completions)
	dur := ""
	if start, ok, _ := unstructured.NestedString(obj.Object, "status", "startTime"); ok {
		if t, err := time.Parse(time.RFC3339, start); err == nil {
			end := time.Now()
			if fin, ok, _ := unstructured.NestedString(obj.Object, "status", "completionTime"); ok {
				if ft, err := time.Parse(time.RFC3339, fin); err == nil {
					end = ft
				}
			}
			dur = age1(end.Sub(t))
		}
	}
	return Row{Name: name, Namespace: ns, DotClass: dot, Cells: []string{name, fmt.Sprintf("%d/%d", succeeded, completions), dur, age}}
}

func cronJobRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	schedule, _, _ := unstructured.NestedString(obj.Object, "spec", "schedule")
	suspend, _, _ := unstructured.NestedBool(obj.Object, "spec", "suspend")
	last, _, _ := unstructured.NestedString(obj.Object, "status", "lastScheduleTime")
	return Row{Name: name, Namespace: ns, Cells: []string{name, schedule, fmt.Sprintf("%v", suspend), last, age}}
}

func serviceRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	typ, _, _ := unstructured.NestedString(obj.Object, "spec", "type")
	clusterIP, _, _ := unstructured.NestedString(obj.Object, "spec", "clusterIP")
	ports, _, _ := unstructured.NestedSlice(obj.Object, "spec", "ports")
	var portStrs []string
	for _, p := range ports {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		port, _, _ := unstructured.NestedInt64(pm, "port")
		proto, _, _ := unstructured.NestedString(pm, "protocol")
		portStrs = append(portStrs, fmt.Sprintf("%d/%s", port, proto))
	}
	return Row{Name: name, Namespace: ns, DotClass: "dot-green", Cells: []string{name, typ, clusterIP, strings.Join(portStrs, ","), age}}
}

func ingressRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	rules, _, _ := unstructured.NestedSlice(obj.Object, "spec", "rules")
	var hosts []string
	for _, r := range rules {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		if h, _, _ := unstructured.NestedString(rm, "host"); h != "" {
			hosts = append(hosts, h)
		}
	}
	return Row{Name: name, Namespace: ns, Cells: []string{name, strings.Join(hosts, ","), age}}
}

func endpointsRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	subsets, _, _ := unstructured.NestedSlice(obj.Object, "subsets")
	count := 0
	for _, s := range subsets {
		sm, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		addrs, _, _ := unstructured.NestedSlice(sm, "addresses")
		count += len(addrs)
	}
	return Row{Name: name, Namespace: ns, Cells: []string{name, fmt.Sprintf("%d addresses", count), age}}
}

func keyedRow(obj *unstructured.Unstructured, name, ns, age, field string) Row {
	data, _, _ := unstructured.NestedMap(obj.Object, field)
	return Row{Name: name, Namespace: ns, Cells: []string{name, fmt.Sprintf("%d", len(data)), age}}
}

func secretRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	typ, _, _ := unstructured.NestedString(obj.Object, "type")
	data, _, _ := unstructured.NestedMap(obj.Object, "data")
	return Row{Name: name, Namespace: ns, Cells: []string{name, typ, fmt.Sprintf("%d", len(data)), age}}
}

func pvcRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	volume, _, _ := unstructured.NestedString(obj.Object, "spec", "volumeName")
	capacity, _, _ := unstructured.NestedString(obj.Object, "status", "capacity", "storage")
	return Row{Name: name, Namespace: ns, DotClass: dotForPhase(phase), Cells: []string{name, phase, volume, capacity, age}}
}

func pvRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	capacity, _, _ := unstructured.NestedString(obj.Object, "spec", "capacity", "storage")
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	claim, _, _ := unstructured.NestedString(obj.Object, "spec", "claimRef", "name")
	return Row{Name: name, Namespace: ns, DotClass: dotForPhase(phase), Cells: []string{name, capacity, phase, claim, age}}
}

func nodeRow(obj *unstructured.Unstructured, name, ns, age string) Row {
	conditions, _, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	status, dot := "Unknown", "dot-gray"
	for _, c := range conditions {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if t, _, _ := unstructured.NestedString(cm, "type"); t == "Ready" {
			if s, _, _ := unstructured.NestedString(cm, "status"); s == "True" {
				status, dot = "Ready", "dot-green"
			} else {
				status, dot = "NotReady", "dot-red"
			}
		}
	}
	labels := obj.GetLabels()
	var roles []string
	for k := range labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
		}
	}
	if len(roles) == 0 {
		roles = []string{"<none>"}
	}
	version, _, _ := unstructured.NestedString(obj.Object, "status", "nodeInfo", "kubeletVersion")
	return Row{Name: name, Namespace: ns, DotClass: dot, Cells: []string{name, status, strings.Join(roles, ","), version, age}}
}

func eventRow(obj *unstructured.Unstructured, name, ns string) Row {
	lastSeen, _, _ := unstructured.NestedString(obj.Object, "lastTimestamp")
	typ, _, _ := unstructured.NestedString(obj.Object, "type")
	reason, _, _ := unstructured.NestedString(obj.Object, "reason")
	message, _, _ := unstructured.NestedString(obj.Object, "message")
	objKind, _, _ := unstructured.NestedString(obj.Object, "involvedObject", "kind")
	objName, _, _ := unstructured.NestedString(obj.Object, "involvedObject", "name")
	dot := "dot-yellow"
	if typ == "Normal" {
		dot = "dot-green"
	} else if typ == "Warning" {
		dot = "dot-red"
	}
	return Row{Name: name, Namespace: ns, DotClass: dot, Cells: []string{lastSeen, typ, reason, objKind + "/" + objName, message}}
}

func dotForPhase(phase string) string {
	switch phase {
	case "Running", "Active", "Bound":
		return "dot-green"
	case "Pending":
		return "dot-yellow"
	case "Failed":
		return "dot-red"
	default:
		return "dot-gray"
	}
}

func age(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return age1(time.Since(t))
}

func age1(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
