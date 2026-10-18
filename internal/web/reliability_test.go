package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rusik69/kui/internal/k8s"
)

func TestAPIRequestTimeoutCancelsHandlerContext(t *testing.T) {
	handler := withAPIRequestTimeout(10*time.Millisecond, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		http.Error(w, "request timed out", http.StatusGatewayTimeout)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api", nil))
	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}
}

func TestLongLivedEndpointsAreNotGivenAPIRequestTimeout(t *testing.T) {
	for _, path := range []string{"/c/context/ns/pods/watch", "/c/context/ns/pods/name/logs", "/c/context/ns/pods/name/exec"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if !isLongLivedRequest(req) {
			t.Errorf("isLongLivedRequest(%q) = false", path)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/c/context/ns/pods/name/exec", nil)
	if isLongLivedRequest(req) {
		t.Fatal("non-GET request was treated as a long-lived stream")
	}
}

func TestStreamEventsRefreshesListOnConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := httptest.NewRecorder()
	(&Server{}).streamEvents(recorder, recorder, ctx, make(chan k8s.RowEvent))
	if got := recorder.Body.String(); !strings.Contains(got, "event: message\ndata: changed\n\n") {
		t.Fatalf("initial stream payload = %q, want initial refresh event", got)
	}
}
