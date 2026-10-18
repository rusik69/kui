package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAttentionSeverityClassifiesKnownFailures(t *testing.T) {
	for _, test := range []struct {
		reason string
		want   string
	}{
		{reason: "Failed", want: "critical"},
		{reason: "CrashLoopBackOff", want: "critical"},
		{reason: "ImagePullBackOff", want: "critical"},
		{reason: "OOMKilled", want: "critical"},
		{reason: "Pending", want: "warning"},
		{reason: "UnknownFutureReason", want: "warning"},
	} {
		t.Run(test.reason, func(t *testing.T) {
			if got := attentionSeverity(test.reason); got != test.want {
				t.Fatalf("attentionSeverity(%q) = %q, want %q", test.reason, got, test.want)
			}
		})
	}
}

func TestGroupEventsAggregatesSortsAndLimits(t *testing.T) {
	now := time.Now().UTC()
	events := []corev1.Event{
		{Type: "Warning", Reason: "BackOff", LastTimestamp: metav1.NewTime(now.Add(-time.Minute))},
		{Type: "Warning", Reason: "BackOff", EventTime: metav1.MicroTime{Time: now.Add(-time.Second)}},
		{Type: "Warning", Reason: "BackOff", ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(now.Add(-time.Hour))}},
		{Type: "Warning", Reason: "Failed", LastTimestamp: metav1.NewTime(now.Add(-time.Minute))},
		{Type: "Warning", Reason: "Failed", LastTimestamp: metav1.NewTime(now.Add(-2 * time.Minute))},
	}
	for i := range 8 {
		events = append(events, corev1.Event{Type: "Normal", Reason: string(rune('A' + i))})
	}

	groups, more := groupEvents(events)
	if len(groups) != 8 {
		t.Fatalf("groupEvents returned %d visible groups, want 8", len(groups))
	}
	if more != 2 {
		t.Fatalf("groupEvents omitted %d groups, want 2", more)
	}
	if groups[0].Type != "Warning" || groups[0].Reason != "BackOff" || groups[0].Count != 3 {
		t.Fatalf("first group = %#v, want aggregated Warning/BackOff count 3", groups[0])
	}
	if groups[0].LatestAge == "unknown" || groups[0].LatestAge == "" {
		t.Fatalf("latest age did not use a populated Kubernetes event timestamp: %#v", groups[0])
	}
	if groups[1].Type != "Warning" || groups[1].Reason != "Failed" || groups[1].Count != 2 {
		t.Fatalf("second group = %#v, want Warning/Failed count 2", groups[1])
	}
	for i := 1; i < len(groups); i++ {
		if groups[i-1].Count < groups[i].Count {
			t.Fatalf("groups are not sorted by descending count: %#v", groups)
		}
	}
}

func TestGroupEventsUsesCreationTimeAndMarksMissingTime(t *testing.T) {
	created := metav1.NewTime(time.Now().UTC().Add(-time.Minute))
	groups, more := groupEvents([]corev1.Event{
		{Type: "Normal", Reason: "Created", ObjectMeta: metav1.ObjectMeta{CreationTimestamp: created}},
		{Type: "Normal", Reason: "UnknownTime"},
	})
	if more != 0 || len(groups) != 2 {
		t.Fatalf("groupEvents returned %d groups and %d omitted, want 2 and 0", len(groups), more)
	}
	byReason := make(map[string]string, len(groups))
	for _, group := range groups {
		byReason[group.Reason] = group.LatestAge
	}
	if byReason["Created"] == "unknown" || byReason["Created"] == "" {
		t.Fatalf("creation timestamp was ignored: %#v", byReason)
	}
	if byReason["UnknownTime"] != "unknown" {
		t.Fatalf("missing timestamp age = %q, want unknown", byReason["UnknownTime"])
	}
}

func TestOverviewUnavailableStateKeepsRefreshControl(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatalf("create web server templates: %v", err)
	}
	data := OverviewData{
		Context: "ctx", Namespace: "_all", UpdatedAt: time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC),
		Error: "Overview data is unavailable. Check cluster access and use Refresh to retry.",
	}
	recorder := httptest.NewRecorder()
	server.render(recorder, "overview", data)
	body := recorder.Body.String()
	for _, want := range []string{`id="overview-error"`, `role="alert"`, `id="overview-refresh"`, "Overview data is unavailable"} {
		if !strings.Contains(body, want) {
			t.Errorf("overview recovery state is missing %q", want)
		}
	}
	if strings.Contains(body, `data-overview-section="health"`) {
		t.Error("unavailable overview rendered zero-valued health metrics as live data")
	}
}
func TestOverviewRetainsHealthWhenEventAPIIsUnavailable(t *testing.T) {
	server, err := New(nil)
	if err != nil {
		t.Fatalf("create web server templates: %v", err)
	}
	data := OverviewData{
		Context: "ctx", Namespace: "_all", UpdatedAt: time.Now().UTC(),
		EventsAvailable: false, EventsError: "Event data is unavailable for this namespace.",
		NodeCount: 2, NodeReadyCount: 1,
	}
	recorder := httptest.NewRecorder()
	server.render(recorder, "overview", data)
	body := recorder.Body.String()
	for _, want := range []string{`data-overview-section="health"`, `id="overview-refresh"`, `role="status"`, "Event data is unavailable"} {
		if !strings.Contains(body, want) {
			t.Errorf("partial overview is missing %q", want)
		}
	}
	if !strings.Contains(body, `<span class="value">1/2</span>`) {
		t.Error("partial event failure discarded available health metrics")
	}
}
