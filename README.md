# kui

A lightweight, local Kubernetes web UI — browse, tail logs, exec into pods,
edit/delete resources and port-forward, all from one small Go binary.

Reads `~/.kube/config` like `kubectl`/`k9s`/Lens — no in-cluster install,
no separate auth. Can additionally import cluster kubeconfigs from a
Rancher management server (see below).

## Run

```sh
go run ./cmd/kui
```

Open the printed `http://127.0.0.1:<port>` URL. Flags:

- `--kubeconfig` path to kubeconfig (defaults to `$KUBECONFIG` / `~/.kube/config`)
- `--addr` TCP listen address (default `127.0.0.1:0`, loopback/free port; external binds require network protections)
- `--context` and `--namespace` override the initial kubeconfig defaults
- `--rancher-url` import cluster kubeconfigs from a Rancher management server (or set `KUI_RANCHER_URL`)
- `--rancher-token` Rancher API bearer token (or set `KUI_RANCHER_TOKEN`; env var preferred — a token on the command line is visible in the process list)
- `--rancher-insecure` skip TLS verification for the Rancher server (self-signed certificates)
- `--debug-image` debug-shell container image (default `busybox:1.36`)
- `--access-log` opt-in HTTP request logging (method, path, status, duration; no query string)
- `--headless` run the HTTP server without a native window; it exits on SIGINT or SIGTERM

### Rancher clusters

Pass `--rancher-url` (plus an API token) and kui imports one context per
Rancher cluster you can access, named `rancher/<cluster name>`. They appear in
the top-bar context switcher and the search palette like any other context;
picking one browses the cluster through Rancher's generated kubeconfig.

```sh
KUI_RANCHER_TOKEN=token-xxxx go run ./cmd/kui --rancher-url https://rancher.example.com
```

- Needs a Rancher API token with at least view access to the clusters
  (Settings → API Keys in the Rancher UI).
- Contexts refresh from Rancher every 5 minutes; a generated kubeconfig is
  reused for 1 hour. Tokens Rancher embeds expire per the server's
  `kubeconfig-default-token-ttl-minutes` setting (default 30 days).
- Imports are in-memory only; the kubeconfig file is never modified.
- If Rancher is unreachable, the last known cluster set keeps working until
  the server responds again.

### Prebuilt release binaries

GitHub releases publish these raw executable assets and matching SHA-256
manifests. Releases produced by the updated workflow also include an SPDX SBOM
(`kui.spdx.json`):

| Platform | Executable | Checksum manifest |
| --- | --- | --- |
| Linux x86_64 | `kui-linux-amd64` | `kui-linux-amd64.sha256` |
| Linux ARM64 | `kui-linux-arm64` | `kui-linux-arm64.sha256` |
| macOS Intel | `kui-darwin-amd64` | `kui-darwin-amd64.sha256` |
| macOS Apple silicon | `kui-darwin-arm64` | `kui-darwin-arm64.sha256` |
| Windows x86_64 | `kui-windows-amd64.exe` | `kui-windows-amd64.exe.sha256` |

On Linux, verify with `sha256sum -c <manifest>`. On macOS, use
`shasum -a 256 -c <manifest>`. On Windows, use
`certutil -hashfile kui-windows-amd64.exe SHA256` and compare the result with
the hash in the `.sha256` manifest.

Releases produced by the updated workflow include signed build-provenance and
SBOM attestations. Verify an executable from one of those releases with
GitHub CLI:

```sh
gh attestation verify kui-linux-amd64 --repo rusik69/kui
```

The release workflow smoke-tests each executable's CLI and health endpoints
before publishing the binaries.

The native webview still requires platform dependencies: GTK 3 and WebKitGTK
4.0 on Linux, and the WebView2 runtime on Windows. macOS uses the system
WebKit webview; it needs no separately installed webview runtime.

## What it does

- Browse Pods/Deployments/ReplicaSets/Jobs/CronJobs/StatefulSets/DaemonSets,
  Services/Ingresses/Endpoints, ConfigMaps/Secrets, PVCs/PVs/StorageClasses,
  Nodes/Namespaces/Events, plus any CRD via live API discovery ("All resources").
- Live-updating tables (watch -> SSE), pod logs with filtering/pause/tail controls,
  interactive pod exec, and an ephemeral pod debug shell with an explicit stop control.
- Open a node debug shell from a node's detail pane. It joins host network/PID/IPC
  namespaces and mounts the host root read-only at `/host`; it is not a privileged container.
- Resize the detail pane by dragging its divider or focusing it and using the
  arrow keys. Shells require a selected namespace and provide visible connection status
  plus a reconnect control.
- Filter resource tables with Kubernetes label/field selectors, sort by clicking
  or keyboard, copy names/namespaces, and page through API results.
  (200 rows by default; choose 1–1,000 rows/page and persist density.)

- Download selected objects as a deterministically ordered, multi-document YAML
  bundle (up to 100 objects and 8 MiB). Bundles include Secret data; protect
  the downloaded file accordingly.
- YAML view/edit-and-apply warns before discarding unsaved changes, validates
  through server-side dry-run, shows API errors inline, and can copy or download
  the current manifest. Resource delete, pod eviction, and port-forward are available.
- Search with the keyboard-accessible command palette; recent destinations are
  saved per browser, and resource navigation supports browser back/forward.

- Open independent workspace tabs with pinned/closed-tab recovery, save scoped
  searches, and export table data as JSON or TSV. Local browser preferences keep
  workspace, query, and table state separate from appearance settings, which
  offer system/light/dark/high-contrast modes, four color palettes, accent color,
  font size, and spacing. These preferences stay separate from Kubernetes
  credentials and object secrets.
- Detail and YAML workbenches provide section controls, copyable identity,
  bounded draft revisions, diff navigation, log bookmarks/highlights, terminal
  controls, and port-forward session status. The overview adds refresh controls,
  capacity thresholds, event drill-down, and a filterable notification center.

- Switch kubeconfig contexts/namespaces from the top bar, search namespaces, and
  retain each tab's context/namespace on refresh. Kubeconfig edits reload contexts
  and rebuild cached clients. Clusters from a Rancher server (`--rancher-url`)
  appear as extra `rancher/<name>` contexts in the same switcher.

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
- Pod debug shells: `list`, `create`, `get`, and `delete` Pods in the selected
  namespace.
- Node debug shells need those Pod verbs plus cluster policy that permits pinning
  a Pod to a node, host namespaces, and a read-only host-root mount. Node shells
  do not receive a service-account token.
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
  `--kubeconfig` overrides the default file and `$KUBECONFIG`. With
  `--rancher-url` set, imported `rancher/<name>` contexts are also available;
  if kui starts with no context selected it picks the first one.
- **Cluster unreachable:** check VPN/network access and the selected context's
  API server address and certificate settings. The page shell remains usable
  when cluster requests fail; the status bar and inline connection error show
  the failure.
- **Expired credentials:** refresh the kubeconfig's credential plugin or
  cloud/SSO login, then retry. kui rereads kubeconfig changes and rebuilds
  cached clients. For Rancher contexts, regenerate the API token if it was
  revoked — kui re-fetches Rancher every 5 minutes and keeps serving the last
  known cluster set while the server is unreachable.
- **Rancher clusters missing:** confirm the token has access to the clusters
  (test with `curl -H "Authorization: Bearer $TOKEN" <rancher-url>/v3/clusters`).
  Clusters still provisioning, with disconnected agents, or without kubeconfig
  permissions are skipped until the next refresh.
- **CPU/memory metrics unavailable:** metrics-server must be installed and
  reachable through the `metrics.k8s.io` API; missing metrics do not prevent
  browsing.
- **Logs, exec, or forwarding denied:** check the corresponding `pods/log`,
  `pods/exec`, or `pods/portforward` permission in the active namespace.
- **Node shell denied:** check Pod create permissions and whether Pod Security
  Admission or other cluster policy permits the requested host namespaces and
  hostPath. The node shell is intentionally unavailable with namespace `All`.

## Non-goals

- SSH protocol access to nodes — kui provides a node debug shell through a pod,
  not SSH credentials or an SSH daemon.
- Multi-user auth / in-cluster deployment — this is a local, single-user tool
  that trusts whatever your kubeconfig already grants.

## Contributing

Install the Go version declared in `go.mod`. Building, testing, and vetting
`cmd/kui` requires cgo and the native webview development libraries: GTK 3 and
WebKitGTK 4.0 development packages on Linux (`libgtk-3-dev` and
`libwebkit2gtk-4.0-dev` on Ubuntu); use the platform's webview development
dependencies on macOS and Windows. The production application does not need
Node.js or npm. The browser verification suite uses Node.js 22 and the pinned
Playwright test dependency as test-only tooling.

Canonical verification commands:

```sh
make ci
npm ci
npx playwright install chromium
make test-ui
```

On Ubuntu, install Chromium's operating-system dependencies with
`npx playwright install --with-deps chromium`. `make ci` runs formatting
checks, `go vet`, lint, race-enabled Go tests with `coverage.out`, and a build.
`make test` runs only the race-enabled Go tests and regenerates the Go coverage
profile. `make test-ui` builds the real `kui` binary and runs the pinned
Playwright Chromium suite against a deterministic local Kubernetes API fixture;
it does not use a cluster or kubeconfig credentials.

## Stack

Go backend (`client-go` + dynamic/discovery clients), HTMX + vanilla JS
frontend (server-rendered `html/template`, no Node/npm production build step),
one Go executable with embedded frontend assets via `go:embed`; native webview
runtimes remain platform dependencies. Node.js and Playwright are used only
for browser tests.
