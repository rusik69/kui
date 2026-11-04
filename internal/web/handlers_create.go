package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/rusik69/kui/internal/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// maxCreateFormBytes bounds the create-dialog manifest payload (form field
// "yaml"); in line with maxYAMLFormBytes for the edit path.
const maxCreateFormBytes = 1 << 20

// handleResourceScaffold serves a minimal starter manifest for the create
// dialog. It is read-only and needs no CSRF protection: nothing is applied
// here, the dialog POSTs to the create endpoint separately. The scaffold
// carries no cluster-derived data beyond the requested resource kind.
func (s *Server) handleResourceScaffold(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource")

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	rk, ok := resolveKind(c, resource)
	if !ok {
		http.Error(w, "unknown resource: "+resource, http.StatusNotFound)
		return
	}

	var sb strings.Builder
	writeScaffold(&sb, rk, scaffoldNamespace(ns, rk))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprint(w, sb.String())
}

// scaffoldNamespace picks the metadata.namespace for a scaffold: the page
// namespace unless it is the All-namespace pseudo-scope or the kind is
// cluster-scoped.
func scaffoldNamespace(ns string, rk k8s.ResourceKind) string {
	if !rk.Namespaced || ns == "" || ns == "_all" {
		return ""
	}
	return ns
}

func writeScaffold(sb *strings.Builder, rk k8s.ResourceKind, namespace string) {
	fmt.Fprintf(sb, "apiVersion: %s\nkind: %s\n", gvrAPIVersion(rk.GVR), rk.Kind)
	sb.WriteString("metadata:\n")
	sb.WriteString("  name: my-" + strings.ToLower(rk.Kind) + "\n")
	if namespace != "" {
		fmt.Fprintf(sb, "  namespace: %s\n", namespace)
	}
	sb.WriteString("  labels:\n    app: my-" + strings.ToLower(rk.Kind) + "\n")
	writeScaffoldSpec(sb, rk)
}

// writeScaffoldSpec appends a kind-specific minimal spec so the scaffold is a
// plausible starting point rather than an empty shell. Unknown kinds (CRDs)
// get a commented placeholder — the server-side dry-run catches everything
// the scaffold leaves out before a real create happens.
func writeScaffoldSpec(sb *strings.Builder, rk k8s.ResourceKind) {
	switch rk.Kind {
	case "Pod":
		sb.WriteString("spec:\n  containers:\n  - name: main\n    image: busybox:1.36\n    command: [\"sleep\", \"3600\"]\n")
	case "Deployment":
		sb.WriteString("spec:\n  replicas: 1\n  selector:\n    matchLabels:\n      app: my-deployment\n  template:\n    metadata:\n      labels:\n        app: my-deployment\n    spec:\n      containers:\n      - name: main\n        image: busybox:1.36\n        command: [\"sleep\", \"3600\"]\n")
	case "StatefulSet":
		sb.WriteString("spec:\n  replicas: 1\n  serviceName: my-statefulset\n  selector:\n    matchLabels:\n      app: my-statefulset\n  template:\n    metadata:\n      labels:\n        app: my-statefulset\n    spec:\n      containers:\n      - name: main\n        image: busybox:1.36\n        command: [\"sleep\", \"3600\"]\n")
	case "DaemonSet":
		sb.WriteString("spec:\n  selector:\n    matchLabels:\n      app: my-daemonset\n  template:\n    metadata:\n      labels:\n        app: my-daemonset\n    spec:\n      containers:\n      - name: main\n        image: busybox:1.36\n        command: [\"sleep\", \"3600\"]\n")
	case "ReplicaSet":
		sb.WriteString("spec:\n  replicas: 1\n  selector:\n    matchLabels:\n      app: my-replicaset\n  template:\n    metadata:\n      labels:\n        app: my-replicaset\n    spec:\n      containers:\n      - name: main\n        image: busybox:1.36\n        command: [\"sleep\", \"3600\"]\n")
	case "Job":
		sb.WriteString("spec:\n  template:\n    metadata:\n      labels:\n        app: my-job\n    spec:\n      restartPolicy: Never\n      containers:\n      - name: main\n        image: busybox:1.36\n        command: [\"echo\", \"hello\"]\n")
	case "CronJob":
		sb.WriteString("spec:\n  schedule: \"*/5 * * * *\"\n  jobTemplate:\n    spec:\n      template:\n        metadata:\n          labels:\n            app: my-cronjob\n        spec:\n          restartPolicy: Never\n          containers:\n          - name: main\n            image: busybox:1.36\n            command: [\"echo\", \"hello\"]\n")
	case "Service":
		sb.WriteString("spec:\n  selector:\n    app: my-service\n  ports:\n  - name: http\n    port: 80\n    targetPort: 8080\n")
	case "Ingress":
		sb.WriteString("spec:\n  rules:\n  - host: example.invalid\n    http:\n      paths:\n      - path: /\n        pathType: Prefix\n        backend:\n          service:\n            name: my-service\n            port:\n              number: 80\n")
	case "ConfigMap":
		sb.WriteString("data:\n  example.key: example-value\n")
	case "Secret":
		sb.WriteString("# stringData values are stored base64-encoded by the API server;\n")
		sb.WriteString("# data requires pre-encoded values.\nstringData:\n  example.key: example-value\n")
	case "PersistentVolumeClaim":
		sb.WriteString("spec:\n  accessModes:\n  - ReadWriteOnce\n  resources:\n    requests:\n      storage: 1Gi\n")
	case "Namespace":
		// metadata.name is enough.
	}
}

func gvrAPIVersion(gvr schema.GroupVersionResource) string {
	if gvr.Group == "" {
		return gvr.Version
	}
	return gvr.Group + "/" + gvr.Version
}

// handleResourceCreate applies a posted manifest as a NEW object (Create,
// not Update): server-side dry-run validation first via ?validate=1, then
// the real create. On success it answers with a toast trigger so the list
// fragment reloads and the new object appears.
func (s *Server) handleResourceCreate(w http.ResponseWriter, r *http.Request) {
	ctxName, ns, resource := r.PathValue("ctx"), r.PathValue("ns"), r.PathValue("resource")
	if !parseFormLimit(w, r, maxCreateFormBytes) {
		return
	}
	rawYAML := r.FormValue("yaml")

	c, err := s.clients(ctxName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	rk, ok := resolveKind(c, resource)
	if !ok {
		http.Error(w, "unknown resource: "+resource, http.StatusNotFound)
		return
	}

	obj := &unstructured.Unstructured{}
	jsonBytes, err := yaml.YAMLToJSON([]byte(rawYAML))
	if err != nil {
		http.Error(w, "invalid YAML: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := obj.UnmarshalJSON(jsonBytes); err != nil {
		http.Error(w, "invalid manifest: "+err.Error(), http.StatusBadRequest)
		return
	}
	if obj.GetKind() != rk.Kind {
		http.Error(w, fmt.Sprintf("manifest kind %q does not match resource kind %q", obj.GetKind(), rk.Kind), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(obj.GetName()) == "" {
		http.Error(w, "manifest must set metadata.name", http.StatusBadRequest)
		return
	}
	// The page namespace selects the dialog's default; the manifest's own
	// metadata.namespace (explicitly set in the editor) wins, mirroring
	// kubectl apply semantics.
	manifestNS := obj.GetNamespace()
	if !rk.Namespaced {
		if manifestNS != "" {
			http.Error(w, "cluster-scoped resource manifest must not set a namespace", http.StatusBadRequest)
			return
		}
	} else if manifestNS == "" {
		manifestNS = scaffoldNamespace(ns, rk)
		if manifestNS == "" {
			http.Error(w, "select a namespace or set metadata.namespace in the manifest", http.StatusBadRequest)
			return
		}
		obj.SetNamespace(manifestNS)
	}

	ri := c.Dynamic.Resource(rk.GVR)
	var riNS dynamic.ResourceInterface = ri
	if rk.Namespaced {
		riNS = ri.Namespace(manifestNS)
	}
	validateOnly := r.URL.Query().Get("validate") == "1"
	if validateOnly {
		if _, err := riNS.Create(r.Context(), obj, metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}}); err != nil {
			status := http.StatusBadGateway
			if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
				status = http.StatusUnprocessableEntity
			}
			http.Error(w, "server-side dry-run failed: "+err.Error(), status)
			return
		}
		triggerToast(w, "success", "Server-side validation passed")
		fmt.Fprint(w, `<span class="dot dot-green"></span> valid`)
		return
	}

	created, err := riNS.Create(r.Context(), obj, metav1.CreateOptions{})
	if err != nil {
		status := http.StatusBadGateway
		if apierrors.IsAlreadyExists(err) {
			status = http.StatusConflict
		} else if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
			status = http.StatusUnprocessableEntity
		}
		http.Error(w, err.Error(), status)
		return
	}
	triggerToast(w, "success", fmt.Sprintf("%s %q created in %s", rk.Kind, created.GetName(), displayScope(created)))
	// Return the detail URL so the client can open the created object. Every
	// interpolated value is HTML-escaped: object names are cluster-controlled
	// data and this fragment is inserted into the dialog via innerHTML.
	target := template.HTMLEscapeString(resourcePath(ctxName, created.GetNamespace(), rk.Name, created.GetName()))
	name := template.HTMLEscapeString(created.GetName())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<span class="dot dot-green"></span> created <a href="%s" hx-get="%s" hx-target="#detail-pane" hx-swap="innerHTML" hx-push-url="%s">%s</a>`, target, target, target, name)
}

func displayScope(obj *unstructured.Unstructured) string {
	if obj.GetNamespace() == "" {
		return "cluster scope"
	}
	return "namespace " + obj.GetNamespace()
}
