# Improvement backlog

Implementation plan: prioritize correctness and security fixes first, then reliability, resource workflows, accessibility, and maintenance. Deliver in verified batches; do not treat a proposed feature as a confirmed defect. Items marked **Audit** are confirmed by source inspection; remaining items are scoped candidate enhancements based on the existing kui UI and workflows.

## Security and request correctness

1. **Done:** Restore Gorilla WebSocket's same-origin protection instead of accepting every browser origin.
2. **Done:** Reject YAML whose kind, name, or namespace does not match the resource URL before applying it.
3. **Done:** Bound form bodies for YAML apply, YAML diff, and bulk deletion before parsing.
4. **Done:** Return watch-setup errors as HTTP failures before committing an SSE response, so untrusted errors are not embedded in SSE event fields.
5. **Done:** Reject malformed resource paths before bulk actions use their components.
6. **Done:** Add a per-server CSRF token to htmx mutations and validate it on all mutating routes.
7. **Done:** Reject mutating requests that do not carry a matching same-origin header.
8. **Done:** Enforce a 1 MiB maximum WebSocket exec input message.
9. **Done:** Serialize WebSocket writes and apply a 10-second write deadline to exec and log streams.
10. **Done:** Return metrics client-setup failures as HTTP errors instead of silently returning an empty 200 response.

## Kubernetes correctness and reliability

11. **Done:** Honor the kubeconfig context namespace when an initial context page omits its namespace; explicit all-namespaces selections remain intact.
12. **Done:** Make debug-shell readiness polling cancellation-aware and report timeout/failure instead of rendering a usable shell.
13. **Done:** Preserve actionable metrics API failures instead of collapsing all errors into unavailable.
14. **Done:** Scope rollout-history ReplicaSet listing by the Deployment selector while retaining the owner-UID check.
15. **Done:** Return watch setup failures before committing a successful stream response.
16. **Done:** Bound non-streaming API handlers to 30 seconds while leaving SSE and WebSocket streams unbounded.
17. **Done:** Refresh the resource list whenever a watch stream reconnects, covering events missed during an expired/closed watch.
18. **Done:** Show watch connection and reconnect state in the status bar.
19. **Done:** Cancel debug-shell readiness work and remove the created pod when a request fails or is cancelled.
20. **Done:** Stop all managed port forwards during application shutdown; keep them independent of browser-tab disconnects.

## Resource browsing and data handling

21. **Done:** Make the empty-table row span every rendered header column.
22. **Done:** Keep bulk-selection IDs and select-all state consistent when filtering rows.
23. **Done:** Scale overview object-count bars relative to the largest listed count.
24. **Done:** Add validated Kubernetes label and field selectors to resource lists.
25. **Done:** Add server-side resource pagination with bounded page sizes and next/previous navigation.
26. **Done:** Preserve label/field selectors, page size, text filter, and column sort when switching namespaces.
27. **Done:** Add a clear-filter action and a distinct no-match table state.
28. **Done:** Add persistent compact, default, and comfortable resource-table density settings.
29. **Done:** Make resource-table columns keyboard-sortable and expose sort direction accessibly.
30. **Done:** Add accessible copy actions for resource names and namespaces.

## YAML and resource actions

31. **Done:** Add YAML identity regression cases for missing and mismatched kind/name/namespace and a valid cluster-scoped object.
32. **Done:** Show YAML editor saved/unsaved state and enable review only for dirty content.
33. **Done:** Warn before discarding dirty YAML content through in-app navigation, detail close, or page unload.
34. **Done:** Preserve YAML editor content and dirty state when diff/apply responses report errors.
35. **Done:** Show server validation errors inline beside the YAML editor, preserving line/location text when the API provides it.
36. **Done:** Add a copy-YAML action.
37. **Done:** Add a download-YAML action for the current editor contents.
38. **Done:** Validate edited manifests with Kubernetes server-side dry-run before presenting the final apply confirmation.
39. **Done:** Include the failed delete/evict operation and resource identity in errors; retain the detail pane when the API rejects the request.
40. **Done:** Verify apply/delete operations emit explicit, resource-specific success feedback through the toast UI.

## Pod logs, terminal, and debug shell

41. **Done:** Resize xterm with the pane and viewport using ResizeObserver plus a window-resize fallback.
42. **Done:** Close stale log WebSockets before replacing the container stream or detail content.
43. **Done:** Add pause/resume and clear controls to the log viewer.
44. **Done:** Filter locally buffered log text without restarting the live stream.
45. **Done:** Configure initial log tail size with server-side validation and a 5,000-line cap.
46. **Done:** Show selected container and log/exec connection state.
47. **Done:** Dispose terminal instances, resize observers, window listeners, and sockets when detail content is replaced.
48. **Done:** Surface exec WebSocket connection errors in the terminal and status label.
49. **Done:** Copy selected terminal text and clear the terminal viewport and scrollback.
50. **Done:** Configure ephemeral debug-shell image through `--debug-image` (default `busybox:1.36`).

## Search and navigation

51. **Done:** Debounce command-palette requests by 300 ms; rapid typing produced one request.
52. **Done:** Abort/replace an in-flight search when a newer input request starts.
53. **Done:** Add accessible dialog/combobox/listbox semantics, keyboard focus handling, and focus restoration.
54. **Done:** Distinguish loading, empty, partial/unavailable, and request-failure search states.
55. **Done:** Include kind, object name, and namespace in resource search results and recent labels.
56. **Done:** Preserve browser back/forward navigation through HTMX URL history.
57. **Done:** Store and reopen the eight most recent palette destinations per browser.
58. **Done:** Configure initial context and namespace with `--context` and `--namespace`.
59. **Done:** Recover invalid or removed context URLs by redirecting to a configured/current available context.
60. **Done:** Show the active context in every page title.

## Context and namespace management

61. **Done:** Reload context and namespace data from kubeconfig on each query.
62. **Done:** Fingerprint kubeconfig contents and invalidate cached Kubernetes clients after edits.
63. **Done:** Reject a removed context's cached clients and recover stale page URLs to an available context.
64. **Done:** Show the active namespace in the breadcrumb.
65. **Done:** Add accessible client-side search to the namespace selector.
66. **Done:** Use the selected context's namespace consistently; contexts without one use the explicit all-namespaces value.
67. **Done:** Keep context and namespace in each tab's URL so refreshes remain tab-local.
68. **Done:** Distinguish the explicit `_all` namespace selection from an unset namespace.
69. **Done:** Keep the page shell available and show namespace-list permission/connection failures inline.
70. **Done:** Cover malformed IDs and unusual kubeconfig context names in tests.

## Accessibility and responsive UI

- **Done:** Add semantic labels to icon-only buttons.
- **Done:** Add keyboard operation for the detail-pane splitter.
- **Done:** Restore focus after closing modal dialogs.
- **Done:** Close dialogs with Escape, trap modal focus, and prevent background interaction while open.
- **Done:** Add visible focus styles for keyboard navigation.
- **Done:** Respect reduced-motion preferences.
- **Done:** Improve narrow-screen layout for table/detail panes.
- **Done:** Make toast announcements available to assistive technology.
- **Done:** Expose accessible sort state and direction on table headers.
- **Done:** Ensure light and dark themes meet readable contrast for status indicators.

## Operations and observability

- **Done:** Gracefully shut down HTTP requests, then stop active port forwards.
- **Done:** Print actionable startup errors for missing current context and kubeconfig issues.
- **Done:** Add `/healthz` process liveness without cluster access.
- **Done:** Add `/readyz` for kubeconfig and selected-context status without cluster access.
- **Done:** Log server start/stop and listener address consistently.
- **Done:** Return a nonzero process exit status on unexpected HTTP server failure without calling Fatal from a goroutine.
- **Done:** Add opt-in request access logging with query values omitted.
- **Done:** Clarify loopback defaults and external exposure in CLI help.
- **Done:** Make headless mode exit cleanly on SIGINT/SIGTERM.
- **Done:** Limit background resource fanout to six concurrent API requests.

## Tests, docs, and maintainability

- **Done:** Add handler tests for malformed context IDs and missing context names.
- **Done:** Add request-body boundary tests for bounded form endpoints.
- **Done:** Test WebSocket cross-origin rejection.
- **Done:** Verify CR/LF-containing Kubernetes watch errors stay ordinary HTTP errors, not SSE frames.
- **Done:** Test request cancellation during debug-shell readiness polling.
- **Done:** Verify filtered bulk-selection transitions in a real browser.
- **Done:** Exercise sort/filter/selection reconciliation in a real browser.
- **Done:** Document permission requirements for browse, logs, exec, port-forward, and mutations.
- **Done:** Document the security model and loopback versus externally bound usage.
- **Done:** Document troubleshooting for unreachable clusters, missing metrics-server, and expired credentials.

## Delivery batches

- Batch 1: items 1–5, security and resource-identity correctness.
- Batch 2: items 6–9, request and WebSocket hardening.
- Batch 3: items 10–15, Kubernetes and stream correctness.
- Continue in priority order; update item state and verify each batch before marking it complete.
Batch 1 is complete. Regression tests cover cross-origin WebSocket rejection, YAML identity matching, oversized forms, malformed bulk resource paths, and pre-stream watch failures. `go test ./...` passed.

Batch 2 is complete. Tests cover origin/token enforcement, malformed origins, oversize exec messages, and WebSocket writes. A browser smoke run verified the rendered CSRF token, htmx request hook, rejection without a token (403), and acceptance with the token (200). `go test ./...` and `go build ./cmd/kui` passed.

Batch 3 is complete. Regression tests cover metrics setup/API errors, configured context namespace selection, debug-shell readiness cancellation, Deployment-selector filtering, and pre-SSE watch errors.

Batch 4 is complete. Tests cover API timeout context propagation, stream timeout exemptions, initial watch refresh, and `ForwardManager.StopAll`. The browser smoke confirmed the status bar changes from connected to reconnecting. `go test ./...`, `go build ./cmd/kui`, and a direct-binary SIGTERM shutdown run passed.
Batch 6, item 24, is complete. Resource list selectors are validated, passed to the Kubernetes API, retained in the list URL, and applied through the table controls. The browser smoke verified selector encoding and HTTP 400 for malformed input; `go test ./...` and `node --check internal/web/static/app.js` passed.
Batch 5 is complete. Tests cover empty-table colspan and relative overview bar widths; browser smoke verified filtered select-all preserves hidden selections and updates the bulk count. `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 7, item 25, is complete. Kubernetes list requests now use API continuation tokens and a validated 1–1,000 row limit (200 default). Browser smoke verified next/previous cursor navigation and page-size changes retain selectors; regression tests cover limit/selector parsing and control rendering. `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 8, items 26–27, is complete. Browser smoke verified namespace changes retain selectors, page size, text filter, sort key/direction, and that clearing a filter restores rows/removes its URL state; empty matches show the no-results row. Regression coverage checks that row; `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 9, item 28, is complete. The table density control persists in local storage; browser smoke measured compact (3px) and comfortable (11px) row padding and verified the setting restores in a new tab. Regression tests cover control rendering; `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 10, item 29, is complete. Resource headers are focusable, Enter/Space sortable, and expose ascending/descending state through `aria-sort`. Browser smoke verified ordering and the ARIA transition; template regression coverage verifies sortable-header semantics. `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 11, item 30, is complete. Resource name and namespace copy buttons have accessible labels and success/failure feedback, with an async Clipboard API path and an execCommand fallback. Browser smoke clicked the action and observed the success toast; template regression coverage checks both copy values and labels. `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 12, item 31, is complete. Regression tests now cover absent and mismatched identity fields plus a valid cluster-scoped manifest; the focused YAML identity tests passed.
Batch 13, item 32, is complete. The YAML editor indicates saved/unsaved state, disables review when unchanged, and returns to saved only after an applied response. Browser smoke verified dirty, successful, and failed apply transitions; template regression coverage checks initial state. `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 14, item 33, is complete. Browser smoke verified in-app request cancellation/confirmation, Escape/close protection, and the beforeunload guard; `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 15, item 34, is complete. Browser smoke changed the draft, simulated an apply failure, and verified the editor and saved baseline remained intact with the unsaved indicator still active.
Batch 16, item 35, is complete. Browser smoke dispatched an API validation failure containing a line location and verified accessible inline output without changing the draft; focused Go tests and `node --check` passed.
Batch 17, item 36, is complete. Browser smoke clicked Copy YAML with a controlled clipboard and verified exact editor contents; focused template tests passed.
Batch 18, item 37, is complete. Browser smoke clicked Download YAML and verified the downloaded blob matched editor contents and used the resource filename; focused template tests passed.
Batch 19, item 38, is complete. A handler integration test sent the edited Deployment through a real dynamic client to a fake Kubernetes API, verified `dryRun=All`, rendered the success state, and surfaced API validation failures without applying. Focused tests and `node --check` passed.
Batch 20, item 39, is complete. A dynamic-client integration test verified a Kubernetes Forbidden response becomes an operation-specific error. Browser smoke confirmed the selected detail stays visible and the failure appears in the toast.
Batch 21, item 40, is complete. A Kubernetes-backed delete handler test verified the success toast carries the resource identity; browser smoke rendered it as a success toast.
Batch 22, items 41–50, is complete. Browser smoke verified terminal resizing, log connection replacement, pause/resume/filter/clear, tail-size clamping, selected-container query state, exec errors, terminal disposal, copy, and clear. Go tests cover rendered pod controls and accepted/rejected tail bounds; a debug-pod test verifies the configured image. `go test ./internal/web ./internal/k8s -count=1`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 23, items 51–60, is complete. Browser smoke confirmed one search request for rapid typing, accessible palette focus/selection, recent destinations, partial API failure state, initial namespace selection, page title, and back-navigation restoration. Handler tests cover configured defaults and stale-context fallback; template tests cover search states and accessible controls. `go test ./internal/web ./internal/k8s -count=1`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 24, items 61–70, is complete. Kubeconfig tests edited contexts and API hosts, then verified list refresh, stale-client rejection, and rebuilt clients. Browser smoke verified the configured context/namespace, namespace filtering, breadcrumb, and visible cluster error while the shell remained usable. `go test ./internal/web ./internal/k8s -count=1`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 25, items 71–80, is complete. Accessibility tests cover icon labels, live toast announcements, dialog semantics, and splitter semantics. Browser smoke verified dialog focus restoration/Escape/Tab trap/inert background, keyboard splitter resizing, visible keyboard focus, a 390px responsive layout, reduced-motion styles, and status-color contrast ratios above 4.5:1 in dark and light themes. `go test ./internal/web -run 'Test(PageRendersAccessibleSearchPalette|PodDetailRendersLogAndTerminalControls|DetailYAMLEditorRendersSavedStateControls)$' -count=1` and `node --check internal/web/static/app.js` passed.
Batch 26, items 81–90, is complete. Go tests cover context selection errors, health/readiness, query-free access logs, and bounded resource fanout. A direct-binary headless smoke returned HTTP 200 from `/healthz` and `/readyz`, logged only `GET /healthz` for a request containing a secret query value, printed its listener URL, handled SIGTERM, logged shutdown, and exited 0. `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Batch 27, items 91–100, is complete. Added context handler fallback, form-body boundary, and CR/LF watch-error regression coverage; existing WebSocket-origin and debug-readiness cancellation tests passed in the full suite. Browser smoke verified filtered select-all preserves hidden selections, synchronizes the count/checkboxes, and reconciles descending sort state after a swap. README now documents permissions, exposure/security, health/readiness semantics, and troubleshooting. `go test ./...`, `go build ./cmd/kui`, and `node --check internal/web/static/app.js` passed.
Post-review fix: debug-shell startup now removes only finished Pods, preserving pending/running sessions across tabs; the detail pane exposes an explicit stop action. README includes the required `list pods` permission. Regression tests cover cleanup phase filtering, safe labeled-pod deletion, and the stop handler; browser smoke verified the stop confirmation dialog.
