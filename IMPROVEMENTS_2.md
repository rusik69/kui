# Improvement batch 2: 100 more improvements

Plan for additional product, reliability, security, and maintenance improvements. Every item has a distinct acceptance condition; mark complete only after implementation and verification. Items build on, and do not repeat, the completed work in `IMPROVEMENTS.md` and `UI_IMPROVEMENTS.md`.

## Kubernetes data and discovery

1. [x] Start resource watches from a list resource version where available, closing the list/watch race.
2. [x] Preserve watch `Error` events instead of dropping them as non-resource objects.
3. [x] Surface watch failures as a safe status distinct from resource refreshes.
4. [x] Apply field selectors consistently to both resource lists and watches.
5. [x] Make same-name resources from different API groups navigable without ambiguity.
6. [x] Filter API-discovered subresources and non-readable resources from the browse directory.
7. [x] Make discovery output deterministic for duplicated API versions.
8. [x] Retain discovery failures alongside partial resource discovery results.
9. [x] Choose the active Deployment history revision from the live Deployment state, not list order.
10. [x] Stabilize rollout-history ordering when revision annotations are absent or tied.
11. [x] Show every regular container image in rollout history.
12. [x] Include init-container images in rollout history.
13. [x] Expose metrics API availability separately from a valid empty metrics response.
14. [x] Filter pod-metrics queries by the current resource selector where possible.
15. [x] Make pod log requests support previous container instances.
16. [x] Make log timestamps an explicit option instead of an unconditional upstream setting.
17. [x] Allow log streams to request a bounded byte range as well as a bounded line tail.
18. [x] Make terminal resize-queue reads stop promptly when the exec session is cancelled.
19. [x] Expose port-forward termination errors to the owning session.
20. [x] Provide a consistent active-port-forward snapshot for state and cleanup.

## HTTP behavior and action safety

21. [x] Return non-success HTTP status codes when the cluster-status endpoint cannot query Kubernetes.
22. [x] Set an explicit text content type on status endpoint responses.
23. [x] Verify status counts and namespace scoping at the handler boundary.
24. [x] Verify health/readiness response bodies and method behavior.
25. [x] Cover readiness failures for missing, removed, and unreadable selected contexts.
26. [x] Return log-stream setup failures as HTTP errors before WebSocket upgrade.
27. [x] Distinguish normal log EOF from upstream stream failures.
28. [x] Surface upstream log failures to the client with an explicit, safe stream error.
29. [x] Reject local and remote port-forward ports outside 1–65535 before creating a tunnel.
30. [x] Return a non-success status for failed port-forward startup while preserving the inline error fragment.
31. [x] Bound and parse port-forward form bodies before allocating work.
32. [x] Treat a debug shell as usable only after its container is ready.
33. [x] Use Kubernetes-generated collision-resistant names for ephemeral debug pods.
34. [x] Clean up debug pods on every failed post-create path while retaining successful sessions.
35. [x] Represent unavailable metrics explicitly rather than as an empty successful fragment.
36. [x] Test metric-fragment JSON escaping against script-context injection.
37. [x] Give unsupported SSE response writers an explicit protocol-level response.
38. [x] Verify watch route headers and initial refresh behavior at the HTTP boundary.
39. [x] Verify watch streams end promptly on cancellation and upstream closure.
40. [x] Buffer template output so an execution error cannot append an HTTP error to partial HTML.

## Resource browsing and table workflows

41. [x] Copy the names of selected resources in displayed order.
42. [x] Export the current visible and sorted resource-table page as correctly escaped CSV.
43. [x] Show selected-versus-visible row counts in the bulk-selection state.
44. [x] Separate “select visible rows” from “select all rows on this page” semantics.
45. [x] Add one reset action for selectors, page size, continuation, and client-side sort/filter state.
46. [x] Filter a resource table by one selected column as well as across all columns.
47. [x] Restore selected row IDs after browser back/forward navigation within a resource list.
48. [x] Keep selection feedback accurate after client-side filtering and sorting.
49. [x] Copy exact values from detail metadata, conditions, and container rows.
50. [x] Retain a return path from a pod opened through a node’s related-pods table.
51. [x] Search within the currently open detail pane without another API request.
52. [x] Filter the Events detail table by visible event fields.
53. [x] Sort Deployment rollout history by its rendered columns with visible direction state.
54. [x] Keep the active resource-table column filter synchronized with the URL.
55. [x] Restore table selector and page-size controls from the URL on refresh.
56. [x] Prevent stale page-continuation tokens from surviving selector or page-size changes.
57. [x] Report when CSV export is empty because no rows are visible.
58. [x] Escape CSV quotes, separators, and line breaks without altering cell content.
59. [x] Buffer bounded log messages received while paused and render them when resumed, without reconnecting.
60. [x] Clear cross-page selection explicitly and update its count immediately.

## Logs, details, and long-lived sessions

61. [x] Add case-sensitive log filtering without reconnecting the stream.
62. [x] Ignore late callbacks from disposed log or exec sessions so they cannot update a replacement detail pane.
63. [x] Add follow mode so reading older log lines does not force-scroll on every update.
64. [x] Provide an explicit jump-to-latest action for paused follow mode.
65. [x] Announce one accessible stream-error state while keeping high-volume log content out of live announcements.
66. [x] Restore the previous log container and tail size when revisiting the same pod.
67. [x] Reset persisted log-container settings safely when a container is no longer present.
68. [x] Add a same-port shortcut to port-forward forms without locking independently editable ports.
69. [x] Verify watch-error SSE notifications change status without refreshing the resource list.
70. [x] Keep Secret reveal state local to the current detail view and clear it when that view closes.
71. [x] Bound the log buffer by both line count and total bytes while retaining whole lines.
72. [x] Keep the log scroll position stable only when follow mode is disabled.
73. [x] Disable stream retry while a connection attempt is already active.
74. [x] Keep stream status text and retry availability synchronized across open/close/error transitions.
75. [x] Restore the selected log container after a stream reconnect.
76. [x] Keep pod container selection valid when a detail fragment is replaced.
77. [x] Clear stale log and terminal session state when switching to a non-Pod detail.
78. [x] Make export filenames include sanitized pod and container identities.
79. [x] Expose a count of buffered log lines and whether older lines were trimmed.
80. [x] Mark log downloads as partial when bounded buffering has trimmed older lines.

## Verification, release integrity, and contributor workflow

81. [x] Generate a reusable Go coverage profile from the standard test target.
82. [x] Publish per-package coverage output from CI without replacing race-enabled tests.
83. [x] Test access-log status recording for implicit and explicit status codes.
84. [x] Exercise the real SSE route through access-log middleware and verify an initial flushed event plus query-free logging.
85. [x] Test escaped request paths while continuing to omit query values from access logs.
86. [x] Exercise readiness against an unreachable API server to ensure it remains kubeconfig-only.
87. [x] Add endpoint-level tests for status counts, content type, and failure semantics.
88. [x] Add route-level SSE tests for headers, event delivery, and termination.
89. [x] Add log-stream tests for pre-upgrade setup errors and post-upgrade read errors.
90. [x] Add table-driven port-forward HTTP tests for boundaries and error statuses.
91. [x] Add debug-shell readiness tests for Running-but-not-ready and terminated containers.
92. [x] Add unit tests for API discovery filtering, partial errors, and deterministic results.
93. [x] Add history tests for tie ordering and multi-container/init-container images.
94. [x] Add tests for previous-log, timestamp, byte-limit, and resize-cancellation options.
95. [x] Add cross-platform release SHA-256 manifests and verify each asset checksum.
96. [x] Separate release compilation from the job that grants release-write permission.
97. [x] Verify release workflow checkout matches the exact published tag before upload.
98. [x] Check that documented release platforms and names match the build matrix.
99. [x] Document the contributor CI prerequisites and canonical verification commands.
100. [x] Document release checksum verification and platform-specific runtime dependencies.

