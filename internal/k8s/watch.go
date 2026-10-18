package k8s

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// RowEvent is one add/modify/delete notification or a watch error.
// Error events have no Object and never represent a resource update.
type RowEvent struct {
	Type   watch.EventType
	Object *unstructured.Unstructured
	Error  error
}

// Watch starts a watch on the given kind/namespace and forwards events on
// the returned channel until ctx is cancelled. Optional list options carry
// the list ResourceVersion and selectors into the watch request.
func Watch(ctx context.Context, c *Clients, rk ResourceKind, namespace string, options ...metav1.ListOptions) (<-chan RowEvent, error) {
	var opts metav1.ListOptions
	if len(options) != 0 {
		opts = options[0]
	}
	ri := c.Dynamic.Resource(rk.GVR)
	var w watch.Interface
	var err error
	if rk.Namespaced && namespace != "" && namespace != "_all" {
		w, err = ri.Namespace(namespace).Watch(ctx, opts)
	} else {
		w, err = ri.Watch(ctx, opts)
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
				event := RowEvent{Type: ev.Type}
				if ev.Type == watch.Error {
					event.Error = watchError(ev.Object)
				} else {
					event.Object, _ = ev.Object.(*unstructured.Unstructured)
					if event.Object == nil {
						continue
					}
				}
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func watchError(obj runtime.Object) error {
	if status, ok := obj.(*metav1.Status); ok {
		return apierrors.FromObject(status)
	}
	return fmt.Errorf("Kubernetes watch error: %v", obj)
}
