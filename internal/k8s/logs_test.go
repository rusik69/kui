package k8s

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestStreamLogsForwardsPreviousTimestampAndByteOptions(t *testing.T) {
	var got url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	clientset, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL, QPS: 100, Burst: 100})
	if err != nil {
		t.Fatal(err)
	}
	tail, bytes := int64(12), int64(4096)
	stream, err := StreamLogs(context.Background(), &Clients{Clientset: clientset}, "default", "demo", LogOptions{
		Container: "app", Previous: true, Timestamps: true, TailLines: &tail, LimitBytes: &bytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if got.Get("previous") != "true" || got.Get("timestamps") != "true" || got.Get("limitBytes") != "4096" || got.Get("tailLines") != "12" {
		t.Fatalf("log query = %v, want previous/timestamp/tail/byte options", got)
	}
}
