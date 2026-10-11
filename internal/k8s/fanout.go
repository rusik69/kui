package k8s

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// KindResult is one Registry kind's list result from FanOutList.
type KindResult struct {
	Kind ResourceKind
	List *unstructured.UnstructuredList
	Err  error
}

// FanOutList lists every kind in Registry concurrently. Callers should wrap
// ctx with a deadline (e.g. context.WithTimeout) so one slow or unreachable
// API group can't stall the whole call — FanOutList itself has no timeout
// opinion, that policy belongs at the call site.
func FanOutList(ctx context.Context, c *Clients, ns string) []KindResult {
	results := make([]KindResult, len(Registry))
	var wg sync.WaitGroup
	for i, rk := range Registry {
		wg.Add(1)
		go func(i int, rk ResourceKind) {
			defer wg.Done()
			list, err := List(ctx, c, rk, ns)
			results[i] = KindResult{Kind: rk, List: list, Err: err}
		}(i, rk)
	}
	wg.Wait()
	return results
}
