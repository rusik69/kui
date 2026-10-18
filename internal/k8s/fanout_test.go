package k8s

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
)

func TestFanOutList(t *testing.T) {
	pod1 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default"}}
	pod2 := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-2", Namespace: "default"}}
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc-1", Namespace: "default"}}

	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme, pod1, pod2, svc)
	c := &Clients{Dynamic: dyn}

	results := FanOutList(context.Background(), c, "default")

	if len(results) != len(Registry) {
		t.Fatalf("expected %d results (one per registry kind), got %d", len(Registry), len(results))
	}

	var podResult, svcResult, deployResult *KindResult
	for i := range results {
		switch results[i].Kind.Name {
		case "pods":
			podResult = &results[i]
		case "services":
			svcResult = &results[i]
		case "deployments":
			deployResult = &results[i]
		}
	}

	if podResult == nil {
		t.Fatal("expected a pods result")
	}
	if podResult.Err != nil {
		t.Errorf("unexpected error listing pods: %v", podResult.Err)
	}
	if podResult.List == nil || len(podResult.List.Items) != 2 {
		t.Errorf("expected 2 pods, got %+v", podResult.List)
	}

	if svcResult == nil || svcResult.Err != nil || len(svcResult.List.Items) != 1 {
		t.Errorf("expected 1 service with no error, got %+v", svcResult)
	}

	// A kind with nothing seeded should come back as an empty list, not an
	// error — one unpopulated kind must never block the others.
	if deployResult == nil {
		t.Fatal("expected a deployments result")
	}
	if deployResult.Err != nil {
		t.Errorf("unpopulated kind should list empty, not error: %v", deployResult.Err)
	}
	if deployResult.List != nil && len(deployResult.List.Items) != 0 {
		t.Errorf("expected 0 deployments, got %d", len(deployResult.List.Items))
	}
}

func TestFanOutListBoundsConcurrentRequests(t *testing.T) {
	dyn := dynamicfake.NewSimpleDynamicClient(clientgoscheme.Scheme)
	var active, maxActive atomic.Int32
	dyn.PrependReactor("list", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
		current := active.Add(1)
		for previous := maxActive.Load(); current > previous; previous = maxActive.Load() {
			if maxActive.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		active.Add(-1)
		return true, &unstructured.UnstructuredList{}, nil
	})

	var wg sync.WaitGroup
	results := make([][]KindResult, 2)
	for caller := range results {
		wg.Add(1)
		go func(caller int) {
			defer wg.Done()
			results[caller] = FanOutList(context.Background(), &Clients{Dynamic: dyn}, "default")
		}(caller)
	}
	wg.Wait()
	for caller, kinds := range results {
		if len(kinds) != len(Registry) {
			t.Fatalf("caller %d: expected %d kind results, got %d", caller, len(Registry), len(kinds))
		}
		for _, result := range kinds {
			if result.Err != nil {
				t.Errorf("caller %d list %s: %v", caller, result.Kind.Name, result.Err)
			}
		}
	}
	if got := maxActive.Load(); got > maxFanOutWorkers {
		t.Errorf("maximum concurrent requests = %d, want at most %d", got, maxFanOutWorkers)
	}
}
