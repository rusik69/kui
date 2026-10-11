package k8s

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// RowEvent is one add/modify/delete notification for a watched list.
type RowEvent struct {
	Type   watch.EventType
	Object *unstructured.Unstructured
}

// Watch starts a watch on the given kind/namespace and forwards events on
// the returned channel until ctx is cancelled. The channel is closed when
// the watch ends (cancellation or upstream error).
func Watch(ctx context.Context, c *Clients, rk ResourceKind, namespace string) (<-chan RowEvent, error) {
	ri := c.Dynamic.Resource(rk.GVR)
	var w watch.Interface
	var err error
	if rk.Namespaced && namespace != "" && namespace != "_all" {
		w, err = ri.Namespace(namespace).Watch(ctx, metav1.ListOptions{})
	} else {
		w, err = ri.Watch(ctx, metav1.ListOptions{})
	}
	if err != nil {
		return nil, err
	}

	out := make(chan RowEvent)
	go func() {
		defer close(out)
		defer w.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.ResultChan():
				if !ok {
					return
				}
				obj, ok := ev.Object.(*unstructured.Unstructured)
				if !ok {
					continue
				}
				select {
				case out <- RowEvent{Type: ev.Type, Object: obj}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
