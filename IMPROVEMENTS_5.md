# UI improvement batch 5: 100 additional complete features

This batch adds distinct end-to-end UI features beyond `UI_IMPROVEMENTS.md`, `IMPROVEMENTS_2.md`, `IMPROVEMENTS_3.md`, and `IMPROVEMENTS_4.md`. A checkbox is complete only when the behavior, persistence/scope rules, failure state, accessibility behavior, and a consumer-visible test are implemented. Cosmetic-only work does not satisfy a feature item.

## Workspace tabs and session navigation
1. [x] Add a workspace tab strip for independently navigable context/namespace/resource views.
2. [x] Open the current route in a new workspace tab without replacing the current view.
3. [x] Switch workspace tabs while keeping each tab's route and list state isolated.
4. [x] Reorder workspace tabs by pointer and keyboard with an announced position.
5. [x] Rename a workspace tab without changing its route.
6. [x] Pin tabs so bulk-close actions cannot remove them.
7. [x] Close a tab with an unsaved YAML draft only after a clear save/discard/cancel decision.
8. [x] Reopen the most recently closed workspace tab with its former scope.
9. [x] Restore the bounded workspace session after reload, rejecting malformed stored routes.
10. [x] Duplicate a workspace tab and provide close-other/close-unpinned actions.
11. [x] Store recent search queries separately from recent destinations and restore a selected query.
12. [x] Save a named search query scoped to its context and namespace.
13. [x] Rename saved queries without losing their query text or scope.
14. [x] Delete a saved query with a reversible undo action.
15. [x] Export/import saved queries as versioned JSON with size and schema checks.
16. [x] Add `status:` query semantics for supported Kubernetes object states.
17. [x] Add `label:` query semantics with validated Kubernetes label selectors.
18. [x] Add `field:` query semantics with validated Kubernetes field selectors.
19. [x] Add exact-name search syntax distinct from substring matching.
20. [x] Provide qualifier suggestions and precise syntax errors without silently broadening a query.
21. [x] Support multi-column sorting with stable tie-breaking.
22. [x] Show sort priority, direction, and accessible names for every active sort key.
23. [x] Remove one sort key or clear all keys without changing filters or selection.
24. [x] Add explicit case-sensitive and regular-expression row-filter modes with safe invalid-pattern feedback.
25. [x] Filter the current page to selected rows without clearing selection.
26. [x] Add per-column filter controls that compose with the global row filter.
27. [x] Show matching, visible, and selected counts consistently after sorting/filtering.
28. [x] Export selected rows as structured JSON with column labels and namespace identity.
29. [x] Copy selected rows as TSV with correct quoting and stable display order.
30. [x] Pin individual rows locally and restore pins only within the same context/resource scope.
31. [x] Collapse and expand individual detail sections with accessible disclosure semantics.
32. [x] Remember collapsed sections per context and resource kind, not per object identity.
33. [x] Expand or collapse all available detail sections in one action.
34. [x] Copy all labels as valid JSON and preserve their exact values.
35. [x] Copy all annotations as valid JSON and preserve multiline values.
36. [x] Turn a label pill into a correctly escaped table selector without losing current namespace.
37. [x] Turn an annotation pill into a safely escaped table filter.
38. [x] Show owner references as links to their live owning resource when addressable.
39. [x] Show resource UID, API version, and resourceVersion in a copyable identity panel.
40. [x] Open the resource's canonical detail URL in a separate browser tab.
41. [x] Autosave YAML drafts locally by context, namespace, resource kind, name, and UID.
42. [x] Restore a saved draft on reopening and clearly distinguish it from server YAML.
43. [x] Discard a local draft only after an explicit confirmation and restore server content.
44. [x] Keep a bounded per-resource draft revision history and restore a chosen revision.
45. [x] Add keyboard find/replace with next/previous match and exact replacement counts.
46. [x] Add a line-number gutter synchronized with editor scrolling and selection.
47. [x] Add a wrap toggle that does not alter the submitted YAML text.
48. [x] Configure indentation width and insert spaces without rewriting existing content.
49. [x] Validate YAML before review/apply and point to line/column for syntax failures.
50. [x] Navigate between diff hunks and announce the active hunk and changed-line count.
51. [x] Offer a configurable overview refresh interval with pause/resume controls.
52. [x] Mark overview data stale when a refresh exceeds its expected interval or fails.
53. [x] Sort resource-count rows by kind, count ascending, or count descending.
54. [x] Filter event summary rows by type, reason, or message locally.
55. [x] Expand a grouped event row to reveal the matching event records.
56. [x] Configure per-metric capacity warning thresholds and persist them per context.
57. [x] Display threshold breaches as labeled warnings, not color-only changes.
58. [x] Copy a concise, namespace-aware cluster health summary.
59. [x] Export the current overview snapshot as JSON with timestamp and scope.
60. [x] Provide a print-optimized overview layout that omits interactive controls.
61. [x] Add a system theme option that follows live OS light/dark changes.
62. [x] Add a high-contrast theme that preserves status distinctions and focus indicators.
63. [x] Add an accent-color picker backed by validated CSS custom properties.
64. [x] Add a bounded global font-scale control with persistent preference.
65. [x] Add a global compact/comfortable spacing preference independent of table density.
66. [x] Add a collapsible desktop sidebar with a persistent, keyboard-operable toggle.
67. [x] Collapse/expand sidebar resource groups independently and persist their state.
68. [x] Add a keyboard-accessible sidebar resource filter with clear and empty states.
69. [x] Export/import appearance preferences with versioning and without cluster data.
70. [x] Add a single reset action for appearance preferences with explicit confirmation.
71. [x] Add a notification center for recent watch errors and user-selected event types.
72. [x] Mark individual notifications read/unread and announce unread count changes.
73. [x] Clear one notification or clear all after confirmation without changing cluster state.
74. [x] Navigate from an object notification to its corresponding live detail pane.
75. [x] Filter notifications by context, namespace, resource kind, and severity.
76. [x] Configure which event severities create visible notifications.
77. [x] Deduplicate repeated notifications by event UID/resourceVersion within a bounded window.
78. [x] Preserve unread notification state across reloads with a strict storage cap.
79. [x] Show notification history unavailable/partial states without hiding current events.
80. [x] Add a quiet mode that suppresses toast popups while retaining notification-center entries.
81. [x] Bookmark a log line and jump back to it while the stream continues.
82. [x] Annotate a bookmarked log line with a user note stored only for that pod/container identity.
83. [x] Highlight multiple user-defined literal log patterns with distinct accessible labels.
84. [x] Show counts for total, matching, and bookmarked log lines as filters change.
85. [x] Export/import log highlight presets without log contents or credentials.
86. [x] Increase/decrease terminal font size with controls and keyboard shortcuts.
87. [x] Toggle terminal full-screen mode and restore the prior detail layout on exit.
88. [x] Confirm before clearing terminal output when a session is active.
89. [x] Show active port-forward sessions in a compact, status-aware panel with stop actions.
90. [x] Copy a port-forward address with protocol, local port, and remote target preserved.
91. [x] Add a focus-visible tab sequence for the workspace strip, including reorder and close controls.
92. [x] Keep workspace tabs usable at 320px with overflow navigation and visible active state.
93. [x] Make table multi-sort operable without pointer input and announce priority changes.
94. [x] Ensure custom themes retain WCAG-readable text/focus contrast for interactive states.
95. [x] Respect reduced-motion preference for all newly introduced transitions and overlays.
96. [x] Add accessible tooltips for icon-only actions that work with focus and touch.
97. [x] Add consistent pending, success, warning, and failure feedback to every new async action.
98. [x] Make new dialogs restore focus and close on Escape without swallowing input keystrokes.
99. [x] Verify all new flows at desktop, tablet, and 320px viewport sizes.
100. [x] Apply a cohesive visual refresh to the workspace, search, table, detail, and overview surfaces and verify both light/dark contrast.

## Verification

- `make ci` passed: formatting, vet, golangci-lint, race-enabled Go tests, and build.
- `KUI_CHROMIUM_EXECUTABLE=... npm run test:ui` passed all 68 browser tests.
- Live fake-cluster smoke: desktop overview at 1280px with no document overflow; mobile detail overlay at 320px spans x=0–320 with no document overflow.
- Browser smoke confirmed one notification center and the accessible shortcut launcher; notification tests assert a single event toast.

