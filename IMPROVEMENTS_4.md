# Reliability, scale, and security plan

This plan follows the completed UI work in `IMPROVEMENTS_3.md`. It prioritizes operational reliability and measurable performance over more cosmetic UI changes.

## Release hardening

- [x] Use Node 24-compatible GitHub Actions versions for checkout, Go/Node setup, and artifact transfer.
- [x] Generate an SPDX JSON SBOM and signed build-provenance/SBOM attestations for release binaries.
- [x] Smoke-test each target binary with a fake kubeconfig and check `/healthz` and `/readyz` before publication.
- [ ] Run the updated release workflow on all five GitHub-hosted platforms; verify all binary checksums, retrieve the SPDX asset, and verify a binary with `gh attestation verify`.

## Lifecycle reliability

- [x] Test that cancelling a Kubernetes watch stops the underlying watch and closes the event channel.
- [x] Retain tests for kubeconfig reload/client invalidation and API request-context cancellation.
- [x] Make browser detail-opening tests wait for a successful detail response before asserting the pane.
- [x] Add an end-to-end case where a context switch happens while detail/search requests are pending; the new context remains selected after the old requests settle.
- [x] Exercise watch disconnect/reconnect and shutdown in browser integration; launch the actual macOS WebView, verify its page load, and close the window to confirm server shutdown.
- [x] Make Enter trigger the row's HTMX detail request directly, remove synthetic initial watch updates from the fixture, and restore focus to the selected row when details close.

## Large-cluster performance

Ten-sample measurements on Apple M4 Pro, Darwin/arm64, with `go test -bench ... -benchmem -benchtime=1s -count=10`, summarized by `benchstat`:

| Benchmark | Baseline | Proposed budget |
| --- | ---: | ---: |
| Build table data from 10,000 Pods | 22.58 ms/op ±8%; 17.75 MiB/op; 180.1k allocs/op | ≤50 ms/op; ≤25 MB/op |
| Search 10,000 Pods | 497.5 µs/op ±2%; 1.281 KiB/op; 9 allocs/op | ≤5 ms/op; ≤16 KB/op |
| Decode one 200-Pod API page | 805.5 µs/op ±4%; 710.8 KiB/op; 13.16k allocs/op | Baseline only |
| Build table data from one 200-Pod API page | 870.9 µs/op ±5%; 845.5 KiB/op; 15.17k allocs/op | Baseline only |

The 10,000-Pod table benchmark is a synthetic stress case; the application normally requests paginated results. Budgets are initial investigation thresholds, not CI gates. The hosted runner label is pinned to Ubuntu 24.04, but its physical CPU can vary; compare only samples from comparable runner classes and investigate sustained regressions above 20%.

- [x] Add repeatable 10,000-object table and search benchmarks.
- [x] Add paginated HTTP API fixture benchmarks for client list decoding and complete table-data construction.
- [x] Run ten benchmark samples on the pinned Ubuntu 24.04 CI runner, summarize with `benchstat`, and retain the baseline artifact for 90 days.

CI baseline artifact from run `39150587561` (Ubuntu 24.04, AMD EPYC 9V45): 10k-Pod table 32.46 ms/op ±5%, 17.75 MiB/op, 180.1k allocs/op; 10k-object search 564.3 µs/op ±3%, 1.281 KiB/op, 9 allocs/op; paginated API list 1.050 ms/op ±3%, 708.7 KiB/op, 13.17k allocs/op; complete table 1.189 ms/op ±1%, 843.5 KiB/op, 15.18k allocs/op.

The local Apple M4 Pro sample is for reference only. Hosted runner hardware may change; keep the baseline as a trend artifact, not a hard performance gate.

## Security boundaries

- [x] Keep batch YAML export bounded to 100 resources and 8 MiB; oversized output fails atomically.
- [x] Test invalid and cross-context export paths; show the Secret-data warning and limits on the export control.
- [x] Retain node-shell assertions for no service-account token, non-privileged container, and read-only host-root mount.
- [x] Retain same-origin and CSRF checks for browser mutations.
- [x] Add handler-level node-shell API denial coverage; verify the denied response renders no session and carries no HTMX view-change headers.
- [ ] Verify the release SBOM and signed attestations from a clean download environment.
