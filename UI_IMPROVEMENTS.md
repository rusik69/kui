# 100 UI Improvements

UI-only plan based on the current templates, `app.css`, and `app.js`. Each item names a distinct, visible interaction, layout, or accessibility refinement; no backend behavior changes are in scope.

## Visual foundation
1. [x] Add theme-aware surface, overlay, and shadow tokens for consistent elevation.
2. [x] Set `color-scheme` per theme so native controls match dark and light mode.
3. [x] Improve font rendering and consistent text selection colors.
4. [x] Standardize body line height and minimum readable text sizing.
5. [x] Give the application shell a stable minimum width without forcing page overflow.
6. [x] Define consistent border radii for cards, inputs, and dialogs.
7. [x] Add a restrained hover transition for interactive surfaces.
8. [x] Improve scrollbar visibility and contrast in both themes.
9. [x] Add a reusable low-elevation shadow for floating UI.
10. [x] Make disabled controls visually distinct and non-interactive.

## Navigation and shell
11. [x] Give the sidebar brand a distinct wordmark treatment.
12. [x] Add a clear active-navigation edge marker.
13. [x] Increase navigation hit targets and row spacing.
14. [x] Align sidebar icons and labels consistently.
15. [x] Keep the top controls available while the content scrolls.
16. [x] Group context and namespace controls visually.
17. [x] Prevent long context names from widening the top bar.
18. [x] Make the namespace filter and picker sizing predictable.
19. [x] Restyle the global search trigger as the primary navigation action.
20. [x] Make breadcrumbs truncate safely while preserving the full label.

## Resource tables
21. [x] Separate table toolbar and selector controls from data rows.
22. [x] Give table headers a clearer hierarchy and stable sticky surface.
23. [x] Make the active sort direction visually prominent in its header.
24. [x] Strengthen row hover feedback without obscuring status colors.
25. [x] Distinguish selected rows with a theme-safe selected surface.
26. [x] Add keyboard-focus feedback for resource rows.
27. [x] Give empty tables a clear, bounded empty-state treatment.
28. [x] Make row copy actions discoverable on touch and keyboard.
29. [x] Allow wide tables to scroll horizontally within their pane.
30. [x] Keep table density controls and pagination controls legible at narrow widths.

## Detail drawer and tabs
31. [x] Add a subtle separation shadow to the open detail drawer.
32. [x] Keep the detail header visible while its body scrolls.
33. [x] Constrain long object names and keep actions reachable.
34. [x] Increase icon-action hit targets while keeping them aligned.
35. [x] Make the tab strip horizontally scrollable without wrapping.
36. [x] Improve selected-tab contrast and active underline weight.
37. [x] Add visible focus styling to tabs.
38. [x] Improve key/value alignment on wide and narrow drawers.
39. [x] Make label and annotation pills wrap and expose full values.
40. [x] Give detail section headings consistent spacing and separation.

## Overview dashboard
41. [x] Add a more legible overview header hierarchy.
42. [x] Give metric cards consistent minimum height and alignment.
43. [x] Make warning and danger cards recognizable beyond text color.
44. [x] Improve metric number sizing and tabular numeral alignment.
45. [x] Increase capacity bar height and visible track contrast.
46. [x] Add a visible threshold treatment for high capacity usage.
47. [x] Align object-count bars and labels across rows.
48. [x] Improve dashboard grid breakpoints for tablet widths.
49. [x] Separate overview sections with consistent vertical rhythm.
50. [x] Make attention and activity tables easier to scan.

## Search, dialogs, and notifications
51. [x] Add a softer, theme-aware backdrop to the search palette.
52. [x] Keep the palette within the visible viewport on short screens.
53. [x] Improve the palette input focus and search-result hierarchy.
54. [x] Show a clearer active result state for keyboard navigation.
55. [x] Separate command and object result groups visually.
56. [x] Make the no-results message explain how to continue.
57. [x] Improve confirmation dialog spacing and responsive sizing.
58. [x] Make destructive confirmation buttons unmistakable in both themes.
59. [x] Give toast notifications a consistent border, shadow, and width.
60. [x] Place notifications where they avoid mobile safe-area controls.

## Logs, terminal, and YAML
61. [x] Group stream controls with a consistent control-bar surface.
62. [x] Improve log-pane contrast and use readable monospace sizing.
63. [x] Preserve log whitespace without breaking long tokens into unreadable fragments.
64. [x] Make log filtering controls fill available narrow space.
65. [x] Present stream connection state as a visible status badge.
66. [x] Theme the terminal pane consistently with the editor surfaces.
67. [x] Add a clear focus boundary to the YAML editor.
68. [x] Make dirty and saved YAML states visually distinct.
69. [x] Improve added, removed, and unchanged diff-line readability.
70. [x] Make port-forward form controls align and wrap cleanly.

## Responsive behavior
71. [x] Add safe horizontal scrolling to the mobile sidebar.
72. [x] Reorder top-bar controls for small screens without clipping.
73. [x] Let resource tables keep readable columns through local overflow.
74. [x] Present mobile details as a bottom sheet rather than a narrow side drawer.
75. [x] Keep detail tabs usable on mobile with horizontal scrolling.
76. [x] Respect device safe areas for the bottom status bar.
77. [x] Limit toast-stack height and allow notification overflow to scroll.
78. [x] Size the palette against dynamic viewport height.
79. [x] Keep dialogs scrollable on short landscape screens.
80. [x] Prevent narrow screens from clipping action buttons and controls.

## Accessibility and keyboard cues
81. [x] Add a skip link to move directly to the primary content.
82. [x] Use a main landmark for the page content.
83. [x] Use a navigation landmark for resource navigation.
84. [x] Expose the current resource in navigation with `aria-current`.
85. [x] Use a native button for the search trigger.
86. [x] Expose the theme toggle state with `aria-pressed`.
87. [x] Give detail tabs proper tablist, tab, and selected-state semantics.
88. [x] Associate each tab panel with its controlling tab.
89. [x] Support left/right arrow navigation between detail tabs.
90. [x] Ensure focus indicators remain visible on all interactive controls.

## Feedback and content clarity
91. [x] Add a screen-reader-only caption to each resource table.
92. [x] Announce filtered-table empty results accessibly.
93. [x] Make empty-state text more useful and visually distinct.
94. [x] Distinguish loading states from static helper text.
95. [x] Show a subtle sort affordance on sortable headers before sorting.
96. [x] Make watch connection state readable without relying on generated CSS text.
97. [x] Improve in-flight request feedback without dimming content excessively.
98. [x] Present cluster connection failures as a clearly announced alert.
99. [x] Give destructive icon actions an unmistakable hover and keyboard-focus treatment.
100. [x] Remove duplicate notification markup so status announcements have one reliable destination.
