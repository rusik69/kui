package web

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rusik69/kui/internal/k8s"
)

// renderOverview mirrors renderAllResourcesDirectory's exact 3-way branching
// (frame=table / HX-Request / full page) so Overview flows through the same
// list-pane fragment mechanism every other resource uses — list-pane's own
// hx-get just happens to land on the stat-tile fragment instead of a table.
func (s *Server) renderOverview(w http.ResponseWriter, r *http.Request, ctxName, ns string, c *k8s.Clients) {
	if r.URL.Query().Get("frame") == "table" {
		data, err := s.buildOverviewData(r.Context(), c, ns)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		data.Context = ctxName
		data.Namespace = ns
		s.render(w, "overview", data)
		return
	}

	page, err := s.buildPageData(r.Context(), ctxName, ns, "overview")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		s.render(w, "content", page)
	} else {
		s.render(w, "page", page)
	}
}

func (s *Server) buildOverviewData(ctx context.Context, c *k8s.Clients, ns string) (OverviewData, error) {
	var d OverviewData

	var (
		nodes    *corev1.NodeList
		nodesErr error

		namespaces []string
		nsErr      error

		pods    *corev1.PodList
		podsErr error

		events *corev1.EventList
	)

	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		nodes, nodesErr = c.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	}()
	go func() {
		defer wg.Done()
		namespaces, nsErr = listNamespaceNames(ctx, c)
	}()
	go func() {
		defer wg.Done()
		pods, podsErr = c.Clientset.CoreV1().Pods(nsOrAll(ns)).List(ctx, metav1.ListOptions{})
	}()
	go func() {
		defer wg.Done()
		events, _ = c.Clientset.CoreV1().Events(nsOrAll(ns)).List(ctx, metav1.ListOptions{})
	}()

	fanCtx, cancel := context.WithTimeout(ctx, fanOutTimeout)
	d.ObjectCounts = countsFromFanOut(k8s.FanOutList(fanCtx, c, ns))
	cancel()

	wg.Wait()
	if nodesErr != nil {
		return d, nodesErr
	}
	if nsErr != nil {
		return d, nsErr
	}
	if podsErr != nil {
		return d, podsErr
	}

	d.NodeCount = len(nodes.Items)
	for _, n := range nodes.Items {
		if nodeReady(n.Status.Conditions) {
			d.NodeReadyCount++
		}
		d.CPUAllocatableMilli += n.Status.Allocatable.Cpu().MilliValue()
		d.MemAllocatableBytes += n.Status.Allocatable.Memory().Value()
		d.PodsAllocatable += n.Status.Allocatable.Pods().Value()
	}

	d.NamespaceCount = len(namespaces)

	tally := tallyPodPhases(pods.Items)
	d.PodRunning, d.PodPending, d.PodFailed = tally.Running, tally.Pending, tally.Unhealthy

	d.PodsUsed = int64(len(pods.Items))
	for _, p := range pods.Items {
		for _, cont := range p.Spec.Containers {
			d.CPURequestedMilli += cont.Resources.Requests.Cpu().MilliValue()
			d.MemRequestedBytes += cont.Resources.Requests.Memory().Value()
		}
		if state := podStateTyped(p); podStateIsBad(state) {
			if len(d.NeedsAttention) < 8 {
				ready := 0
				for _, cs := range p.Status.ContainerStatuses {
					if cs.Ready {
						ready++
					}
				}
				detail := strconv.Itoa(ready) + "/" + strconv.Itoa(len(p.Spec.Containers))
				d.NeedsAttention = append(d.NeedsAttention, AttentionItem{
					Name: p.Name, Namespace: p.Namespace, Reason: state, Detail: detail,
				})
			}
		}
	}

	if events != nil {
		var warnings []corev1.Event
		for _, ev := range events.Items {
			if ev.Type == corev1.EventTypeWarning {
				warnings = append(warnings, ev)
			}
		}
		d.WarningCount = len(warnings)
		sort.Slice(warnings, func(i, j int) bool {
			return warnings[i].LastTimestamp.Time.After(warnings[j].LastTimestamp.Time)
		})
		for i, ev := range warnings {
			if i >= 5 {
				break
			}
			d.RecentWarnings = append(d.RecentWarnings, Event{
				Type: ev.Type, Reason: ev.Reason, Message: ev.Message, Age: age(ev.LastTimestamp.Time),
			})
		}

		activity := append([]corev1.Event{}, events.Items...)
		sort.Slice(activity, func(i, j int) bool {
			return activity[i].LastTimestamp.Time.After(activity[j].LastTimestamp.Time)
		})
		for i, ev := range activity {
			if i >= 8 {
				break
			}
			d.RecentActivity = append(d.RecentActivity, Event{
				Type: ev.Type, Reason: ev.Reason, Message: ev.Message, Age: age(ev.LastTimestamp.Time),
			})
		}
	}

	return d, nil
}

// nodeReady extracts the shared Ready-condition check also used by
// columns.go's nodeRow, so the two stay consistent.
func nodeReady(conditions []corev1.NodeCondition) bool {
	for _, c := range conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// nsOrAll maps kui's "_all" namespace sentinel to the typed clientset's
// cross-namespace value.
func nsOrAll(ns string) string {
	if ns == "_all" {
		return metav1.NamespaceAll
	}
	return ns
}

// podStateTyped mirrors podStateFromUnstructured (columns.go) for callers
// using the typed Clientset — CrashLoopBackOff/ImagePullBackOff pods stay at
// phase=Running, so this walks ContainerStatuses the same way to catch them.
func podStateTyped(p corev1.Pod) string {
	state := string(p.Status.Phase)
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
			state = cs.State.Waiting.Reason
		} else if cs.State.Terminated != nil && cs.State.Terminated.Reason != "" {
			state = cs.State.Terminated.Reason
		}
	}
	return state
}

type podTally struct {
	Running, Pending, Failed, Succeeded, Other, Unhealthy int
}

// tallyPodPhases is shared by buildOverviewData and the status-bar endpoint
// so the two never drift on what counts as "running"/"failing".
func tallyPodPhases(pods []corev1.Pod) podTally {
	var t podTally
	for _, p := range pods {
		switch p.Status.Phase {
		case corev1.PodRunning:
			t.Running++
		case corev1.PodPending:
			t.Pending++
		case corev1.PodFailed:
			t.Failed++
		case corev1.PodSucceeded:
			t.Succeeded++
		default:
			t.Other++
		}
		if podStateIsBad(podStateTyped(p)) {
			t.Unhealthy++
		}
	}
	return t
}
