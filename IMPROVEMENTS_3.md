# UI improvement batch 3: 100 complete features

This plan is distinct from `IMPROVEMENTS.md`, `UI_IMPROVEMENTS.md`, and `IMPROVEMENTS_2.md`. Each item describes observable, end-to-end behavior; a checklist item is complete only after its behavior and failure paths are implemented and tested. Cosmetic changes alone do not satisfy an item.

## Table layout and personal views
1. [x] Add a column chooser that shows the active resource's available columns.
2. [x] Allow optional columns to be hidden and restored without losing row data.
3. [x] Keep the resource name column visible and explain why it cannot be hidden.
4. [x] Reorder columns with pointer drag and preserve row/header alignment.
5. [x] Reorder columns using keyboard controls with an announced position.
6. [x] Resize columns with a minimum readable width and persist the width.
7. [x] Persist table layouts separately by context and resource kind.
8. [x] Preview layout changes immediately before leaving the chooser.
9. [x] Restore the resource's default layout without affecting other resources.
10. [x] Show the number of visible columns and announce layout changes accessibly.

## Saved resource views
11. [x] Save selectors, text/column filters, sorting, page size, density, and column layout as a named view.
12. [x] Apply a saved view and restore its complete filter and layout state.
13. [x] Update an existing view from the current table state.
14. [x] Rename a view without duplicating or losing its state.
15. [x] Delete a view only after an explicit confirmation.
16. [x] Duplicate a view to create an independently editable variant.
17. [x] Export views as versioned JSON with no credentials or cluster data.
18. [x] Import views with schema, size, and unsafe-selector validation.
19. [x] Scope views to context, namespace, and resource kind; never apply a view to a different scope.
20. [x] Show the active view and unsaved changes, and return cleanly to the default view.

## Resource comparison
21. [x] Compare two to four selected resources in a dedicated comparison panel.
22. [x] Align common metadata and spec fields by stable field path.
23. [x] Highlight changed values and show absent-versus-empty distinctly.
24. [x] Toggle between all fields and changed fields only.
25. [x] Compare labels and annotations by key, including missing keys.
26. [x] Compare container names, images, readiness, and restart counts.
27. [x] Expand nested values without truncating the underlying comparison.
28. [x] Copy the comparison as readable text and structured JSON.
29. [x] Export comparison rows to correctly escaped CSV.
30. [x] Clear or replace comparison members without clearing unrelated table selection.

## Batch metadata workflows
31. [x] Add a label to all selected resources with Kubernetes key/value validation.
32. [x] Update a selected label across the batch with an explicit preview.
33. [x] Remove a chosen label from selected resources.
34. [x] Add an annotation to selected resources with multiline value support.
35. [x] Update or remove a chosen annotation across selected resources.
36. [x] Preview the exact per-resource metadata patch before submission.
37. [x] Confirm batch scope and affected resource identities before mutation.
38. [x] Report per-resource success and failure for partial API outcomes.
39. [x] Retry only failed resources without replaying successful patches.
40. [x] Download selected resources as a deterministically ordered YAML bundle.

## Deployment operations
41. [x] Scale a Deployment to a validated replica count from its detail pane.
42. [x] Reject negative, non-integer, and out-of-range replica values before mutation.
43. [x] Show desired/current/available replicas together during a scale operation.
44. [x] Restart a Deployment rollout with a fresh, collision-safe restart annotation.
45. [x] Pause a Deployment rollout with an explicit state and confirmation.
46. [x] Resume a paused Deployment rollout and report API errors inline.
47. [x] Roll back to a selected available revision, not an implicit list-order choice.
48. [x] Show rollout progress and terminal conditions from live Deployment status.
49. [x] Preserve the detail pane and selected revision when an operation fails.
50. [x] Add backend tests for action authorization boundaries, patch payloads, conflicts, and partial status.

## Log and terminal workbench
51. [x] Select whether logs include timestamps and apply it to reconnects.
52. [x] Fetch logs from the previous container instance with a visible mode indicator.
53. [x] Select a bounded “since” time window and preserve it on reconnect.
54. [x] Offer named tail presets and retain a validated custom line count.
55. [x] Toggle wrapping for long log lines without altering copied or downloaded text.
56. [x] Search logs with literal or regular-expression mode and report invalid expressions safely.
57. [x] Copy the current filtered log result and preserve line breaks.
58. [x] Download raw or filtered logs with a filename that identifies pod, container, and mode.
59. [x] Choose the exec container explicitly and restore that choice only while it exists.
60. [x] Display reconnect attempts, next retry delay, and a cancelable retry state.

## Overview personalization and operations
61. [x] Add a namespace-aware health summary with an explicit all-namespaces state.
62. [x] Filter attention items by severity and retain the filter during refresh.
63. [x] Navigate from every health count to its matching resource selector.
64. [x] Pin and unpin metric tiles without changing the underlying cluster data.
65. [x] Reorder overview sections and persist the arrangement per context.
66. [x] Hide or restore optional overview sections with a reversible settings panel.
67. [x] Show when metric values were last refreshed and when the API is unavailable.
68. [x] Refresh overview data on demand and prevent overlapping refresh requests.
69. [x] Offer an event summary grouped by type and reason with links to matching events.
70. [x] Preserve an accessible loading, empty, partial, and failure state for each section.

## Search and navigation
71. [x] Parse `kind:`, `namespace:`, and `name:` search qualifiers with quoted values.
72. [x] Show active query filters as removable chips.
73. [x] Filter results by resource kind without another server request.
74. [x] Preserve query and filter state when opening and returning from a result.
75. [x] Show result counts per kind and distinguish truncated results.
76. [x] Add a command-palette shortcut reference that reflects the active platform.
77. [x] Navigate between recent destinations with keyboard controls and clear individual entries.
78. [x] Pin favorite destinations separately from recents and reorder favorites.
79. [x] Keep the current context/namespace when a search command navigates to another resource.
80. [x] Display a useful recovery action for empty, partial, and failed search results.

## Keyboard and accessibility workflows
81. [x] Add a discoverable keyboard-shortcuts dialog with conflict-free global shortcuts.
82. [x] Move focus to the resource table, detail pane, and navigation landmarks by shortcut.
83. [x] Move between table rows with arrow keys while preserving native checkbox behavior.
84. [x] Toggle row selection with Space and announce selected/total-visible counts.
85. [x] Open and close a focused row's detail pane with keyboard controls.
86. [x] Restore focus to the originating row after closing its detail pane.
87. [x] Keep dialogs keyboard-operable with focus trapping, Escape behavior, and focus restoration.
88. [x] Announce asynchronous table, search, comparison, and batch-operation outcomes once.
89. [x] Respect reduced motion and forced-colors preferences across new controls.
90. [x] Verify keyboard and screen-reader names for every new interactive control.

## Visual polish and responsive behavior
91. [x] Establish consistent spacing, typography, and control sizing for all new workbench surfaces.
92. [x] Give popovers and comparison panels theme-aware elevation and predictable stacking.
93. [x] Add skeleton loading states matching table, overview, and detail content geometry.
94. [x] Replace blank failures with compact recovery states that preserve the surrounding layout.
95. [x] Add clear hover, pressed, focus, disabled, and pending states for interactive controls.
96. [x] Make tables, comparison panels, and popovers usable at 320px viewport width.
97. [x] Keep destructive and host-level actions visually distinct in light and dark themes.
98. [x] Use consistent status colors plus text/icons so state never depends on color alone.
99. [x] Keep visual transitions within reduced-motion and performance constraints.
100. [x] Verify the finished overview, table, detail, and operation surfaces in both themes and at desktop/mobile widths.
