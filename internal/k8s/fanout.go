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

// FanOutList lists every kind in Registry concurrently, via the
// watch-cache-serving ListFromCache. A fixed worker pool and shared semaphore
// bound API requests across concurrent callers. Callers should wrap ctx with
// a deadline; FanOutList itself has no timeout policy.
const maxFanOutWorkers = 6

var fanOutSlots = make(chan struct{}, maxFanOutWorkers)

func FanOutList(ctx context.Context, c *Clients, ns string) []KindResult {
	results := make([]KindResult, len(Registry))
	jobs := make(chan int, len(Registry))
	for i := range Registry {
		jobs <- i
	}
	close(jobs)

	workers := min(len(Registry), maxFanOutWorkers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for i := range jobs {
				rk := Registry[i]
				select {
				case fanOutSlots <- struct{}{}:
				case <-ctx.Done():
					results[i] = KindResult{Kind: rk, Err: ctx.Err()}
					continue
				}
				list, err := ListFromCache(ctx, c, rk, ns)
				<-fanOutSlots
				results[i] = KindResult{Kind: rk, List: list, Err: err}
			}
		}()
	}
	wg.Wait()
	return results
}
