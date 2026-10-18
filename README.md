# kui

A lightweight, local Kubernetes web UI — browse, tail logs, exec into pods,
edit/delete resources and port-forward, all from one small Go binary.

Reads `~/.kube/config` like `kubectl`/`k9s`/Lens — no in-cluster install,
no separate auth.

## Run

```sh
go run ./cmd/kui
```

Open the printed `http://127.0.0.1:<port>` URL. Flags:

- `--kubeconfig` path to kubeconfig (defaults to `$KUBECONFIG` / `~/.kube/config`)
- `--addr` TCP listen address (default `127.0.0.1:0`, loopback/free port; external binds require network protections)
- `--context` and `--namespace` override the initial kubeconfig defaults
- `--debug-image` debug-shell container image (default `busybox:1.36`)
- `--access-log` opt-in HTTP request logging (method, path, status, duration; no query string)
- `--headless` run the HTTP server without a native window; it exits on SIGINT or SIGTERM

### Prebuilt release binaries

GitHub releases provide one executable per platform: Linux x86_64/ARM64,
macOS Intel/Apple silicon, and Windows x86_64. Download the matching binary
directly; no archive extraction is required. The native webview still requires
platform libraries: GTK 3 and WebKitGTK 4.0 on Linux, and the WebView2 runtime
on Windows.

## What it does

- Browse Pods/Deployments/ReplicaSets/Jobs/CronJobs/StatefulSets/DaemonSets,
  Services/Ingresses/Endpoints, ConfigMaps/Secrets, PVCs/PVs/StorageClasses,
  Nodes/Namespaces/Events, plus any CRD via live API discovery ("All resources").
- Live-updating tables (watch -> SSE), pod logs with filtering/pause/tail controls,
  interactive pod exec, and an ephemeral debug-pod shell with an explicit stop control.
- Resize the detail pane by dragging its divider or focusing it and using the
  arrow keys. Terminal & SSH requires a selected namespace and provides visible
  connection status plus a reconnect control.
- Filter resource tables with Kubernetes label/field selectors, sort by clicking
  or keyboard, copy names/namespaces, and page through API results.
  (200 rows by default; choose 1–1,000 rows/page and persist density.)
- YAML view/edit-and-apply warns before discarding unsaved changes, validates
  through server-side dry-run, shows API errors inline, and can copy or download
  the current manifest. Resource delete, pod eviction, and port-forward are available.
- Search with the keyboard-accessible command palette; recent destinations are
  saved per browser, and resource navigation supports browser back/forward.

- Switch kubeconfig contexts/namespaces from the top bar, search namespaces, and
  retain each tab's context/namespace on refresh. Kubeconfig edits reload contexts
  and rebuild cached clients.

- Use keyboard-operable dialogs and pane resizing; the interface adapts to narrow
  screens, respects reduced-motion preferences, and uses high-contrast status colors.


## Kubernetes permissions

kui uses the identity and permissions in the selected kubeconfig context.
Grant only the verbs required for the actions users need:

- Browsing and live updates: `get`, `list`, and `watch` on the resource types
  shown; `list` namespaces for namespace selection; API discovery access for
  the "All resources" view.
- YAML editing: `get` and `update` on the target resource. Validation uses
  Kubernetes server-side dry-run; the final save is an update.
- Delete: `delete` on the selected resource. Pod eviction uses `create` on
  `pods/eviction`.
- Pod logs, exec, and port-forward: `get` on `pods/log`, `create` on
  `pods/exec`, and `create` on `pods/portforward`, respectively.
- Debug shells: `list`, `create`, `get`, and `delete` Pods in the selected
  namespace.
- Resource metrics are optional and require `get` on
  `pods.metrics.k8s.io` and `nodes.metrics.k8s.io`.

The exact resource permissions depend on which API types and actions are used.
Kubernetes RBAC and API discovery can also require non-resource discovery
access; consult the cluster administrator for the cluster's policy.

## Security and network exposure

kui is a local, single-user tool. It has no user authentication, TLS
termination, or per-user authorization; every browser action uses the selected
kubeconfig identity. The default `--addr 127.0.0.1:0` binds only to loopback
and selects a free port. Binding `0.0.0.0` or another non-loopback address
exposes the UI to that network; do so only behind trusted network controls and
an authenticated TLS proxy. Same-origin and CSRF checks protect browser
mutations from cross-site requests; they are not authentication for remote
clients. For remote use, prefer headless mode bound to loopback and an SSH
tunnel.

`/healthz` reports that the HTTP process is serving. `/readyz` checks that the
kubeconfig can be read and the selected context exists; neither endpoint
probes the Kubernetes API.

## Troubleshooting

- **No usable context:** repair the kubeconfig or pass `--context NAME`.
  `--kubeconfig` overrides the default file and `$KUBECONFIG`.
- **Cluster unreachable:** check VPN/network access and the selected context's
  API server address and certificate settings. The page shell remains usable
  when cluster requests fail; the status bar and inline connection error show
  the failure.
- **Expired credentials:** refresh the kubeconfig's credential plugin or
  cloud/SSO login, then retry. kui rereads kubeconfig changes and rebuilds
  cached clients.
- **CPU/memory metrics unavailable:** metrics-server must be installed and
  reachable through the `metrics.k8s.io` API; missing metrics do not prevent
  browsing.
- **Logs, exec, or forwarding denied:** check the corresponding `pods/log`,
  `pods/exec`, or `pods/portforward` permission in the active namespace.

## Non-goals

- Real SSH into nodes — no generic node-level credential story exists; the
  "Terminal & SSH" nav item launches an ephemeral shell pod (configurable with
  `--debug-image`) instead.
- Multi-user auth / in-cluster deployment — this is a local, single-user tool
  that trusts whatever your kubeconfig already grants.

## Stack

Go backend (`client-go` + dynamic/discovery clients), HTMX + vanilla JS
frontend (server-rendered `html/template`, no Node/npm build step), one Go
executable with embedded frontend assets via `go:embed`; native webview runtimes
remain platform dependencies.
