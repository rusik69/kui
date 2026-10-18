package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rusik69/kui/internal/k8s"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/rest"
)

func BenchmarkBuildTableData10kPods(b *testing.B) {
	pods, ok := k8s.ByName("pods")
	if !ok {
		b.Fatal("pods resource kind is missing")
	}
	objects := make([]runtime.Object, 10_000)
	for i := range objects {
		objects[i] = &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      fmt.Sprintf("pod-%05d", i),
				"namespace": "benchmark",
			},
			"status": map[string]any{"phase": "Running"},
		}}
	}
	clients := &k8s.Clients{Dynamic: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), objects...)}
	server := &Server{}
	options := v1.ListOptions{Limit: 10_000}
	ctx := context.Background()

	initial, err := server.buildTableData(ctx, clients, pods, "benchmark", "benchmark", options)
	if err != nil {
		b.Fatal(err)
	}
	if len(initial.Rows) != len(objects) {
		b.Fatalf("table returned %d rows, want %d", len(initial.Rows), len(objects))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		table, err := server.buildTableData(ctx, clients, pods, "benchmark", "benchmark", options)
		if err != nil {
			b.Fatal(err)
		}
		if len(table.Rows) != len(objects) {
			b.Fatalf("table returned %d rows, want %d", len(table.Rows), len(objects))
		}
	}
}

func newPaginatedAPIFixture(b *testing.B) (*k8s.Clients, k8s.ResourceKind, v1.ListOptions, int) {
	b.Helper()
	pods, ok := k8s.ByName("pods")
	if !ok {
		b.Fatal("pods resource kind is missing")
	}
	const pageSize = 200
	items := make([]map[string]any, pageSize)
	for i := range items {
		items[i] = map[string]any{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]any{
				"name":      fmt.Sprintf("pod-%03d", i),
				"namespace": "benchmark",
			},
			"status": map[string]any{"phase": "Running"},
		}
	}
	response, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "PodList",
		"metadata":   map[string]any{"continue": "next-page"},
		"items":      items,
	})
	if err != nil {
		b.Fatal(err)
	}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/namespaces/benchmark/pods" {
			http.Error(w, "unexpected API request: "+r.Method+" "+r.URL.String(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	b.Cleanup(api.Close)

	// Prevent client-go's default rate limiter from dominating the local API benchmark.
	dyn, err := dynamic.NewForConfig(&rest.Config{Host: api.URL, QPS: 1_000_000, Burst: 1_000_000})
	if err != nil {
		b.Fatal(err)
	}
	return &k8s.Clients{Dynamic: dyn}, pods, v1.ListOptions{Limit: pageSize}, pageSize
}

func BenchmarkListPaginatedAPIFixture(b *testing.B) {
	clients, pods, options, wantRows := newPaginatedAPIFixture(b)
	list, err := clients.Dynamic.Resource(pods.GVR).Namespace("benchmark").List(context.Background(), options)
	if err != nil {
		b.Fatal(err)
	}
	if got := len(list.Items); got != wantRows {
		b.Fatalf("API list returned %d rows, want %d", got, wantRows)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		list, err := clients.Dynamic.Resource(pods.GVR).Namespace("benchmark").List(context.Background(), options)
		if err != nil {
			b.Fatal(err)
		}
		if len(list.Items) != wantRows {
			b.Fatalf("API list returned %d rows, want %d", len(list.Items), wantRows)
		}
	}
}

func BenchmarkBuildTableDataPaginatedAPIFixture(b *testing.B) {
	clients, pods, options, wantRows := newPaginatedAPIFixture(b)
	server := &Server{}
	table, err := server.buildTableData(context.Background(), clients, pods, "benchmark", "benchmark", options)
	if err != nil {
		b.Fatal(err)
	}
	if len(table.Rows) != wantRows || table.NextContinue != "next-page" {
		b.Fatalf("table returned %d rows with continuation %q, want %d rows and next-page", len(table.Rows), table.NextContinue, wantRows)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		table, err := server.buildTableData(context.Background(), clients, pods, "benchmark", "benchmark", options)
		if err != nil {
			b.Fatal(err)
		}
		if len(table.Rows) != wantRows || table.NextContinue != "next-page" {
			b.Fatalf("table returned %d rows with continuation %q, want %d rows and next-page", len(table.Rows), table.NextContinue, wantRows)
		}
	}
}
