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
- `--addr` listen address (default `127.0.0.1:0`, random free port)

## What it does

- Browse Pods/Deployments/ReplicaSets/Jobs/CronJobs/StatefulSets/DaemonSets,
  Services/Ingresses/Endpoints, ConfigMaps/Secrets, PVCs/PVs/StorageClasses,
  Nodes/Namespaces/Events, plus any CRD via live API discovery ("All resources").
- Live-updating tables (watch -> SSE), pod log tailing, interactive pod exec
  shell, and a one-click ephemeral debug-pod shell ("Terminal & SSH").
- YAML view/edit-and-apply, delete, pod eviction, and port-forward (binds a
  local port directly, since kui runs on your machine).
- Switch kubeconfig contexts/namespaces from the top bar; multiple browser
  tabs can point at different clusters at once.

## Non-goals

- Real SSH into nodes — no generic node-level credential story exists; the
  "Terminal & SSH" nav item launches a `busybox` debug pod instead.
- Multi-user auth / in-cluster deployment — this is a local, single-user tool
  that trusts whatever your kubeconfig already grants.

## Stack

Go backend (`client-go` + dynamic/discovery clients), HTMX + vanilla JS
frontend (server-rendered `html/template`, no Node/npm build step), single
static binary via `go:embed`.
