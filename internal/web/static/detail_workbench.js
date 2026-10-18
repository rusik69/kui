(() => {
  const $ = (selector, root = document) => root.querySelector(selector);
  const $$ = (selector, root = document) => Array.from(root.querySelectorAll(selector));
  const MAX_DRAFT_BYTES = 256 * 1024;
  const MAX_REVISIONS = 10;
  const MAX_BOOKMARKS = 100;
  const MAX_HIGHLIGHTS = 20;

  function header() { return $('#detail-pane .detail-header'); }
  function identity() { const h = header(); return h ? JSON.stringify([h.dataset.context, h.dataset.namespace, h.dataset.resourceKind, $('strong', h)?.textContent || '', h.dataset.resourceUid || '']) : ''; }
  function setStatus(text) {
    const status = $('#yaml-dirty-status');
    if (status) status.textContent = text;
  }
  function initSections() {
    const root = $('#tab-details');
    if (!root || root.dataset.sectionsReady) return;
    root.dataset.sectionsReady = 'true';
    $$('.section-title', root).forEach((title, index) => {
      const button = document.createElement('button');
      button.type = 'button'; button.className = 'section-title section-disclosure';
      button.textContent = title.textContent.trim();
      button.setAttribute('aria-expanded', 'true');
      const controlled = document.createElement('div');
      controlled.className = 'detail-section-content';
      controlled.id = `detail-section-${index}`;
      button.setAttribute('aria-controls', controlled.id);
      let node = title.nextSibling;
      title.replaceWith(button);
      while (node && !(node.nodeType === Node.ELEMENT_NODE && node.classList.contains('section-title'))) {
        const next = node.nextSibling; controlled.appendChild(node); node = next;
      }
      button.after(controlled);
      button.addEventListener('click', () => {
        const expanded = button.getAttribute('aria-expanded') === 'true';
        button.setAttribute('aria-expanded', String(!expanded)); controlled.hidden = expanded;
        saveCollapsed();
      });
    });
    const key = collapseKey();
    try {
      const collapsed = JSON.parse(localStorage.getItem(key) || '[]');
      if (Array.isArray(collapsed)) $$('.section-disclosure', root).forEach((button, i) => {
        if (!collapsed.includes(button.textContent)) return;
        button.setAttribute('aria-expanded', 'false'); $(`#detail-section-${i}`, root).hidden = true;
      });
    } catch { /* Optional local preferences. */ }
  }
  function collapseKey() {
    const h = header(); return `kui.detail-sections:${h?.dataset.context || ''}:${h?.dataset.resourceKind || ''}`;
  }
  function saveCollapsed() {
    try { localStorage.setItem(collapseKey(), JSON.stringify($$('.section-disclosure[aria-expanded="false"]', $('#tab-details')).map(x => x.textContent).slice(0, 100))); } catch { /* Storage is optional. */ }
  }
  function toggleDetailSections(expand) {
    initSections();
    $$('.section-disclosure', $('#tab-details')).forEach(button => {
      button.setAttribute('aria-expanded', String(expand)); $(`#${CSS.escape(button.getAttribute('aria-controls'))}`).hidden = !expand;
    });
    saveCollapsed();
  }
  function copyIdentity() {
    const h = header(); if (!h) return;
    const result = { context: h.dataset.context, namespace: h.dataset.namespace, resource: h.dataset.resourceKind, name: $('strong', h)?.textContent || '', uid: h.dataset.resourceUid || '', apiVersion: $('.resource-identity dl dd:nth-of-type(2)', h)?.textContent || '', resourceVersion: $('.resource-identity dl dd:nth-of-type(3)', h)?.textContent || '' };
    window.kui.copyText(JSON.stringify(result, null, 2));
  }
  function copyMetadata(kind) {
    const row = $(`[data-metadata-kind="${kind}"]`); if (!row) return;
    const data = Object.fromEntries($$('.metadata-pill', row).map(p => [p.dataset.key, p.dataset.value]));
    window.kui.copyText(JSON.stringify(data, null, 2));
  }
  function selectorPill(button) {
    const kind = button.closest('[data-metadata-kind]')?.dataset.metadataKind;
    const h = header(); if (!kind || !h) return;
    const url = new URL(`/c/${encodeURIComponent(h.dataset.context)}/${encodeURIComponent(h.dataset.namespace || '_')}/${encodeURIComponent(h.dataset.resourceKind)}`, location.origin);
    if (kind === 'labels') url.searchParams.set('labelSelector', `${button.dataset.key}=${button.dataset.value}`);
    else url.searchParams.set('rowFilter', `${button.dataset.key}=${button.dataset.value}`);
    location.href = url.pathname + url.search;
  }
  function draftKey() { return `kui.yaml-draft:${identity()}`; }
  function updateLines(editor) {
    const gutter = $('#yaml-line-numbers'); if (!gutter) return;
    const count = editor.value.split('\n').length;
    const activeLine = editor.value.slice(0, editor.selectionStart).split('\n').length;
    gutter.replaceChildren();
    for (let i = 1; i <= count; i++) {
      const line = document.createElement('span'); line.textContent = String(i); line.dataset.line = String(i);
      if (i === activeLine) line.className = 'current-line'; gutter.append(line); if (i < count) gutter.append(document.createTextNode('\n'));
    }
    gutter.scrollTop = editor.scrollTop;
  }
  function saveDraft() {
    const editor = $('#yaml-editor'); if (!editor || header()?.dataset.secret === 'true') return;
    const text = editor.value;
    if (new Blob([text]).size > MAX_DRAFT_BYTES) { setStatus('Draft exceeds the 256 KiB local limit; not saved'); return; }
    const key = draftKey();
    try {
      const old = JSON.parse(localStorage.getItem(key) || '{}');
      if (old.draft !== text) {
        const revisions = Array.isArray(old.revisions) ? old.revisions.filter(x => typeof x === 'string' && x.length <= MAX_DRAFT_BYTES) : [];
        if (typeof old.draft === 'string' && old.draft !== text) revisions.unshift(old.draft);
        const state = { version: 1, draft: text, revisions: revisions.slice(0, MAX_REVISIONS) };
        localStorage.setItem(key, JSON.stringify(state)); populateRevisions(state.revisions);
      }
      setStatus(editor.value === $('input[name="original"]', editor.form)?.value ? 'Server YAML' : 'Local draft saved');
    } catch { setStatus('Local draft could not be saved'); }
  }
  function populateRevisions(revisions) {
    const select = $('#yaml-revisions'); if (!select) return;
    select.replaceChildren(new Option('Current draft', ''));
    revisions.forEach((_, i) => select.add(new Option(`Revision ${i + 1}`, String(i))));
  }
  function initYAML() {
    const editor = $('#yaml-editor'); if (!editor || editor.dataset.workbenchReady) return;
    editor.dataset.workbenchReady = 'true';
    const key = draftKey();
    if (header()?.dataset.secret !== 'true') {
      try {
        const saved = JSON.parse(localStorage.getItem(key) || 'null');
        if (saved && saved.version === 1 && typeof saved.draft === 'string' && saved.draft.length <= MAX_DRAFT_BYTES && Array.isArray(saved.revisions) && saved.revisions.length <= MAX_REVISIONS && saved.revisions.every(x => typeof x === 'string' && x.length <= MAX_DRAFT_BYTES)) {
          editor.value = saved.draft; populateRevisions(saved.revisions); window.kui.syncYAMLDirtyState(); setStatus('Restored local draft · server YAML unchanged');
        } else if (saved !== null) localStorage.removeItem(key);
      } catch { localStorage.removeItem(key); }
    }
    editor.addEventListener('input', () => { updateLines(editor); window.kui.syncYAMLDirtyState(); saveDraft(); });
    editor.addEventListener('scroll', () => updateLines(editor));
    editor.addEventListener('select', () => updateLines(editor));
    $('#yaml-wrap')?.addEventListener('change', e => { editor.wrap = e.target.checked ? 'soft' : 'off'; editor.classList.toggle('yaml-wrap', e.target.checked); });
    updateLines(editor);
    editor.form?.addEventListener('submit', event => { if (new Blob([editor.value]).size > MAX_DRAFT_BYTES) { event.preventDefault(); setStatus('YAML exceeds the 256 KiB review limit'); } });
    $('#yaml-diff-pane')?.addEventListener('htmx:afterSwap', initDiffNavigation);
    initDiffNavigation();
  }
  function initDiffNavigation() {
    const pane = $('#yaml-diff-pane'); if (!pane) return;
    const lines = $$('.diff-line', pane); if (!lines.length || pane.querySelector('.diff-navigation')) return;
    const groups = []; let current = null;
    lines.forEach(line => {
      if (line.classList.contains('diff-eq')) { current = null; return; }
      if (!current) { current = { first: line, changed: 0 }; groups.push(current); }
      current.changed++;
    });
    if (!groups.length) return;
    groups.forEach((group, index) => { group.first.dataset.diffHunk = ''; group.first.dataset.changedLines = String(group.changed); group.first.dataset.hunkIndex = String(index); });
    const controls = document.createElement('div'); controls.className = 'diff-navigation'; controls.setAttribute('role', 'group'); controls.setAttribute('aria-label', 'Diff hunk navigation');
    const previous = document.createElement('button'); previous.type = 'button'; previous.className = 'btn'; previous.textContent = 'Previous hunk'; previous.addEventListener('click', () => navigateDiff(-1));
    const next = document.createElement('button'); next.type = 'button'; next.className = 'btn'; next.textContent = 'Next hunk'; next.addEventListener('click', () => navigateDiff(1));
    const status = document.createElement('span'); status.id = 'diff-hunk-status'; status.setAttribute('role', 'status'); status.setAttribute('aria-live', 'polite'); status.textContent = `${groups.length} diff hunks`;
    controls.append(previous, next, status); pane.prepend(controls);
  }
  function navigateDiff(direction) {
    const hunks = $$('[data-diff-hunk]', $('#yaml-diff-pane')); if (!hunks.length) return;
    const active = Number($('#diff-hunk-status')?.dataset.active || (direction > 0 ? -1 : 0));
    const index = (active + direction + hunks.length) % hunks.length; const hunk = hunks[index];
    hunk.scrollIntoView({ block: 'center' }); const status = $('#diff-hunk-status');
    status.dataset.active = String(index); status.textContent = `Hunk ${index + 1} of ${hunks.length}; ${hunk.dataset.changedLines} changed lines`;
  }
  function findYAML(direction) {
    const editor = $('#yaml-editor'), query = $('#yaml-find')?.value; if (!editor || !query) return;
    const from = direction > 0 ? editor.selectionEnd : editor.selectionStart - 1;
    let at = direction > 0 ? editor.value.indexOf(query, from) : editor.value.lastIndexOf(query, from);
    if (at < 0) at = direction > 0 ? editor.value.indexOf(query) : editor.value.lastIndexOf(query);
    if (at < 0) { setStatus('No YAML match found'); return; }
    editor.focus(); editor.setSelectionRange(at, at + query.length); setStatus('YAML match selected');
  }
  function replaceYAML() {
    const editor = $('#yaml-editor'), query = $('#yaml-find')?.value; if (!editor || !query) return;
    const matches = editor.value.split(query).length - 1;
    if (!matches) { setStatus('Replaced 0 matches'); return; }
    editor.value = editor.value.split(query).join($('#yaml-replace')?.value || '');
    editor.dispatchEvent(new Event('input', { bubbles: true })); setStatus(`Replaced ${matches} matches`);
  }
  function indentYAML() {
    const editor = $('#yaml-editor'); if (!editor) return;
    const startLine = editor.value.lastIndexOf('\n', Math.max(0, editor.selectionStart - 1)) + 1;
    const endLine = editor.value.indexOf('\n', editor.selectionEnd);
    const end = endLine < 0 ? editor.value.length : endLine;
    const width = Math.max(1, Math.min(8, Number($('#yaml-indent')?.value) || 2));
    const part = editor.value.slice(startLine, end).split('\n').map(line => ' '.repeat(width) + line).join('\n');
    editor.setRangeText(part, startLine, end, 'select'); editor.dispatchEvent(new Event('input', { bubbles: true }));
  }
  function restoreRevision() {
    const select = $('#yaml-revisions'), editor = $('#yaml-editor'); if (!select?.value || !editor) return;
    try { const saved = JSON.parse(localStorage.getItem(draftKey()) || '{}'); const revision = saved.revisions?.[Number(select.value)]; if (typeof revision === 'string') { editor.value = revision; editor.dispatchEvent(new Event('input', { bubbles: true })); } } catch { setStatus('Revision history is unavailable'); }
  }
  function hasYAMLDraft() {
    if (header()?.dataset.secret === 'true') return false;
    try { const saved = JSON.parse(localStorage.getItem(draftKey()) || 'null'); return !!saved && saved.version === 1 && typeof saved.draft === 'string'; } catch { return false; }
  }
  function discardDraft(confirmed = false) {
    if (!confirmed && !confirm('Discard the local YAML draft and restore server YAML?')) return false;
    const editor = $('#yaml-editor'); if (!editor) { try { localStorage.removeItem(draftKey()); return true; } catch { return false; } }
    try { localStorage.removeItem(draftKey()); } catch { setStatus('Local YAML draft could not be cleared'); return false; }
    editor.value = $('input[name=\"original\"]', editor.form)?.value || '';
    populateRevisions([]); updateLines(editor); window.kui.syncYAMLDirtyState(); setStatus('Server YAML restored');
    return true;
  }
  function markYAMLApplied() {
    const editor = $('#yaml-editor');
    try { localStorage.removeItem(draftKey()); }
    catch { setStatus('Applied, but the local draft could not be cleared'); return false; }
    const original = editor && $('input[name=\"original\"]', editor.form);
    if (editor && original) original.value = editor.value;
    populateRevisions([]);
    if (editor) window.kui.syncYAMLDirtyState();
    setStatus('Applied · local draft cleared');
    return true;
  }
  async function validateYAML() {
    const editor = $('#yaml-editor'); if (!editor) return;
    const result = $('#yaml-result'); result.textContent = 'Validating…';
    try {
      const response = await fetch(editor.form.getAttribute('hx-put'), { method: 'PUT', headers: { 'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8', 'X-Kui-CSRF': $('meta[name="kui-csrf-token"]')?.content || '' }, body: new URLSearchParams({ original: $('input[name="original"]', editor.form).value, yaml: editor.value }) });
      const body = await response.text(); result.textContent = response.ok ? 'YAML parsed; review available' : `YAML validation failed: ${body.replace(/<[^>]*>/g, '').trim()}`;
    } catch { result.textContent = 'YAML validation request failed'; }
  }
  function initLogs() {
    const pane = $('#logs-pane'); if (!pane || pane.dataset.workbenchReady) return;
    pane.dataset.workbenchReady = 'true';
    try {
      const data = loadLogData();
      const patterns = data.patterns;
      $('#log-highlight-patterns').value = patterns.join('\n'); renderBookmarks(data.bookmarks);
    } catch { /* Invalid local workbench state is ignored. */ }
    pane.addEventListener('click', () => { pane.dataset.selectedLine = String(Math.max(0, Math.floor((pane.scrollTop + pane.clientHeight / 2) / 18))); });
    pane.addEventListener('scroll', () => { pane.dataset.selectedLine = String(Math.max(0, Math.floor((pane.scrollTop + pane.clientHeight / 2) / 18))); });
    new MutationObserver(() => { updateLogCount(); applyHighlights(); }).observe(pane, { childList: true, characterData: true });
    updateLogCount();
  }
  function logKey() { return `kui.log-workbench:${identity()}:${$('#log-container')?.value || ''}`; }
  function loadLogData() {
    try {
      const raw = localStorage.getItem(logKey()) || '{}';
      if (raw.length > 128 * 1024) { localStorage.removeItem(logKey()); return {}; }
      const value = JSON.parse(raw);
      if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
      value.bookmarks = Array.isArray(value.bookmarks) ? value.bookmarks.filter(x => x && Number.isInteger(x.line) && x.line >= 0 && typeof x.note === 'string' && x.note.length <= 500).slice(-MAX_BOOKMARKS) : [];
      value.patterns = Array.isArray(value.patterns) ? value.patterns.filter(x => typeof x === 'string' && x.length <= 200).slice(0, MAX_HIGHLIGHTS) : [];
      return value;
    } catch { return {}; }
  }
  function saveLogData(data) {
    try {
      const encoded = JSON.stringify(data);
      if (encoded.length <= 128 * 1024) localStorage.setItem(logKey(), encoded);
    } catch { /* Storage is optional. */ }
  }
  function logBufferInfo() {
    return window.kui.getLogBufferInfo?.() || { text: window.kui.getLogBuffer?.() || '', baseLine: 0, totalLines: 0 };
  }
  function rawLogLines() {
    const info = logBufferInfo();
    if (!info.text) return [];
    const lines = info.text.split('\n');
    if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop();
    return lines;
  }
  function logLineMatches(line) {
    const query = $('#log-filter')?.value || '';
    if (!query) return true;
    const sensitive = $('#log-case-sensitive')?.checked;
    if ($('#logs-regex')?.checked) { try { return new RegExp(query, sensitive ? '' : 'i').test(line); } catch { return false; } }
    return (sensitive ? line : line.toLowerCase()).includes(sensitive ? query : query.toLowerCase());
  }
  function visibleLogIndices(lines = rawLogLines()) {
    const result = [];
    lines.forEach((line, index) => { if (logLineMatches(line)) result.push(index); });
    return result;
  }
  function updateLogCount() {
    const pane = $('#logs-pane'), out = $('#log-line-counts'); if (!pane || !out) return;
    const lines = rawLogLines(), info = logBufferInfo();
    const matching = visibleLogIndices(lines).length;
    const total = info.totalLines;
    const data = loadLogData(); out.textContent = `${total} total · ${matching} matching · ${(data.bookmarks || []).length} bookmarked`;
  }
  function renderBookmarks(items) {
    const host = $('#log-bookmarks'); if (!host) return;
    host.replaceChildren(); (Array.isArray(items) ? items.slice(0, MAX_BOOKMARKS) : []).forEach((item, index) => {
      if (!item || !Number.isInteger(item.line) || item.line < 0 || typeof item.note !== 'string') return;
      const button = document.createElement('button'); button.type = 'button'; button.className = 'btn log-bookmark'; button.textContent = `Line ${item.line + 1}${item.note ? ` — ${item.note}` : ''}`;
      button.addEventListener('click', () => {
        const info = logBufferInfo(), rawIndex = item.line - info.baseLine, lines = rawLogLines();
        if (rawIndex < 0 || rawIndex >= lines.length) { $('#log-status').textContent = 'Bookmarked line is outside the current log buffer'; return; }
        const visibleIndex = visibleLogIndices(lines).indexOf(rawIndex);
        if (visibleIndex < 0) { $('#log-status').textContent = 'Bookmarked line is hidden by the current filter'; return; }
        $('#logs-pane').scrollTop = visibleIndex * 18;
      });
      host.append(button);
    });
  }
  function bookmarkLog() {
    const pane = $('#logs-pane'), lines = rawLogLines(); if (!lines.length) return;
    const info = logBufferInfo(), visible = visibleLogIndices(lines);
    if (!visible.length) { $('#log-status').textContent = 'No matching log line to bookmark'; return; }
    const selected = Number.parseInt(pane.dataset.selectedLine || '', 10);
    const visibleIndex = Number.isInteger(selected) ? Math.min(selected, visible.length - 1) : visible.length - 1;
    const rawIndex = visible[visibleIndex];
    const data = loadLogData(), bookmarks = Array.isArray(data.bookmarks) ? data.bookmarks : [];
    bookmarks.push({ line: info.baseLine + rawIndex, note: ($('#log-bookmark-note')?.value || '').slice(0, 500) });
    data.bookmarks = bookmarks.slice(-MAX_BOOKMARKS); saveLogData(data); renderBookmarks(data.bookmarks); updateLogCount();
  }
  function saveHighlights() {
    const patterns = ($('#log-highlight-patterns')?.value || '').split('\n').map(x => x.trim()).filter(Boolean);
    if (patterns.length > MAX_HIGHLIGHTS || patterns.some(x => x.length > 200)) { $('#log-status').textContent = 'Use at most 20 literal patterns, each 200 characters or fewer'; return; }
    const data = loadLogData(); data.patterns = patterns; saveLogData(data);
    const pane = $('#logs-pane'); if (pane) pane.textContent = pane.textContent;
    applyHighlights();
  }
  function applyHighlights() {
    const pane = $('#logs-pane'); if (!pane || !$('#log-highlight-patterns')) return;
    const text = pane.textContent, patterns = ($('#log-highlight-patterns').value || '').split('\n').filter(Boolean).slice(0, MAX_HIGHLIGHTS);
    if (!patterns.length || pane.querySelector('mark.log-highlight')) return;
    const expression = new RegExp(patterns.map(x => x.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|'), 'g');
    const fragment = document.createDocumentFragment(); let last = 0; let match;
    while ((match = expression.exec(text))) { fragment.append(document.createTextNode(text.slice(last, match.index))); const mark = document.createElement('mark'); mark.className = `log-highlight log-highlight-${patterns.indexOf(match[0]) % 5}`; mark.setAttribute('aria-label', `Highlighted pattern: ${match[0]}`); mark.textContent = match[0]; fragment.append(mark); last = expression.lastIndex; }
    fragment.append(document.createTextNode(text.slice(last))); if (last > 0) pane.replaceChildren(fragment);
  }
  function exportHighlights() {
    const data = { version: 1, patterns: ($('#log-highlight-patterns')?.value || '').split('\n').filter(Boolean).slice(0, MAX_HIGHLIGHTS) };
    downloadJSON('kui-log-highlights.json', data);
  }
  async function importHighlights(input) {
    try {
      const file = input.files?.[0]; if (!file || file.size > 16 * 1024) throw new Error('Preset file exceeds 16 KiB');
      const value = JSON.parse(await file.text());
      if (!value || value.version !== 1 || !Array.isArray(value.patterns) || value.patterns.length > MAX_HIGHLIGHTS || value.patterns.some(x => typeof x !== 'string' || !x.trim() || x.length > 200)) throw new Error('Invalid highlight preset schema');
      $('#log-highlight-patterns').value = value.patterns.join('\n'); saveHighlights();
    } catch (error) { $('#log-status').textContent = error.message; } finally { input.value = ''; }
  }
  function downloadJSON(filename, object) {
    const a = document.createElement('a'), url = URL.createObjectURL(new Blob([JSON.stringify(object, null, 2)], { type: 'application/json' })); a.href = url; a.download = filename; a.click(); URL.revokeObjectURL(url);
  }
  function zoomTerminal(delta) {
    const terminal = window.kuiTerminalSession?.term; if (!terminal) return;
    terminal.options.fontSize = Math.max(8, Math.min(28, terminal.options.fontSize + delta));
    try { localStorage.setItem('kui.terminal-font-size', String(terminal.options.fontSize)); } catch { /* optional */ }
  }
  function toggleFullscreen() {
    const pane = $('#term-pane'), button = $('[onclick="kui.toggleTerminalFullscreen()"]'); if (!pane) return;
    pane.classList.toggle('terminal-fullscreen'); const active = pane.classList.contains('terminal-fullscreen'); button?.setAttribute('aria-pressed', String(active));
    button.textContent = active ? 'Exit full screen terminal' : 'Full screen terminal'; if (!active) pane.focus();
  }
  function clearTerminal() {
    const session = window.kuiTerminalSession; if (!session) return;
    if (session.ws?.readyState === WebSocket.OPEN && !confirm('A terminal session is active. Clear visible terminal output?')) return;
    session.term.write('\x1b[2J\x1b[3J\x1b[H');
  }
  function init() { initSections(); initYAML(); initLogs(); }
  document.addEventListener('DOMContentLoaded', init);
  if (window.htmx?.onLoad) window.htmx.onLoad(init);
  document.addEventListener('keydown', event => {
    if (event.key === 'Escape' && $('#term-pane')?.classList.contains('terminal-fullscreen')) {
      event.preventDefault(); event.stopImmediatePropagation(); toggleFullscreen();
    }
  }, true);
  document.addEventListener('keydown', event => {
    if (event.target.matches('input,textarea,[contenteditable="true"]')) return;
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'f' && $('#yaml-editor') && !$('#tab-yaml').hidden) { event.preventDefault(); $('#yaml-find')?.focus(); }
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'h' && $('#yaml-editor') && !$('#tab-yaml').hidden) { event.preventDefault(); $('#yaml-replace')?.focus(); }
    if (event.key === '+' || event.key === '=') zoomTerminal(1); if (event.key === '-') zoomTerminal(-1);
  });
  Object.assign(window.kui, {
    toggleDetailSections, copyResourceIdentity: copyIdentity, copyMetadataJSON: copyMetadata, applyMetadataPill: selectorPill,
    findYAML, replaceYAML, indentYAML, restoreYAMLRevision: restoreRevision, discardYAMLDraft: discardDraft, hasYAMLDraft, validateYAML,
    bookmarkCurrentLogLine: bookmarkLog, saveLogHighlights: saveHighlights, exportLogHighlights: exportHighlights, importLogHighlights: importHighlights,
    zoomTerminal, toggleTerminalFullscreen: toggleFullscreen, clearTerminal, markYAMLApplied,
  });
})();
