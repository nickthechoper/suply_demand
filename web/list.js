const tableWrapEl = document.getElementById('table-wrap');
const tableBodyEl = document.getElementById('table-body');
const emptyNoteEl = document.getElementById('empty-note');
const searchEl = document.getElementById('search');
const themeFilterEl = document.getElementById('theme-filter');
const sectorFilterEl = document.getElementById('sector-filter');
const countEl = document.getElementById('count');
const noteModalEl = document.getElementById('note-modal');
const noteTitleEl = document.getElementById('note-title');
const noteTextEl = document.getElementById('note-text');
const noteErrorEl = document.getElementById('note-error');
const noteHintEl = document.getElementById('note-hint');
const noteSaveEl = document.getElementById('note-save');

let rows = [];
let zoneFilter = 'all';
let themeFilter = 'all';
let sectorFilter = 'all';
let query = '';
let sortKey = 'symbol';
let sortDir = 1;
let noteSymbol = null;

function fmt(n) {
  return n === undefined || n === null || Number.isNaN(n) ? '–' : n.toFixed(2);
}

function fmtPct(n) {
  return n === undefined || n === null || Number.isNaN(n) ? '–' : `${n.toFixed(2)}%`;
}

function pctClass(n) {
  return n === undefined || n === null || Number.isNaN(n) ? '' : n >= 0 ? 'pos' : 'neg';
}

function fmtNetUsd(n) {
  if (n === undefined || n === null || Number.isNaN(n)) return '–';
  if (n === 0) return '$0';
  const a = Math.abs(n);
  const short = a >= 1e9 ? `${(a / 1e9).toFixed(2)}B` : a >= 1e6 ? `${(a / 1e6).toFixed(2)}M` : a >= 1e3 ? `${(a / 1e3).toFixed(1)}K` : a.toFixed(0);
  return `${n > 0 ? '+' : '−'}$${short}`;
}

function zonePill(zone) {
  const label = zone === 'supply' ? 'Supply' : zone === 'demand' ? 'Demand' : '—';
  return `<span class="zone-pill ${zone}">${label}</span>`;
}

function ratingLabel(key) {
  if (!key) return '–';
  return key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function flagBadges(row) {
  if (!row.flags || row.flags.length === 0) return '–';
  return row.flags.map(f => `<span class="flag-badge">${f}</span>`).join('');
}

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function noteCell(row) {
  const note = row.note || '';
  if (!note) return `<button class="note-btn empty" type="button">+ Add note</button>`;
  return `<button class="note-btn" type="button" title="${escapeHtml(note)}">${escapeHtml(note.split('\n')[0])}</button>`;
}

function showNoteError(msg) {
  noteErrorEl.textContent = msg;
  noteErrorEl.classList.toggle('hidden', !msg);
  noteHintEl.classList.toggle('hidden', !!msg);
}

function openNote(symbol) {
  const row = rows.find(r => r.symbol === symbol);
  if (!row) return;
  noteSymbol = symbol;
  noteTitleEl.innerHTML = `${escapeHtml(symbol)} <span class="muted">${escapeHtml(row.name || '')}</span>`;
  noteTextEl.value = row.note || '';
  showNoteError('');
  noteSaveEl.disabled = false;
  noteModalEl.classList.remove('hidden');
  noteTextEl.focus();
}

function closeNote() {
  noteSymbol = null;
  noteModalEl.classList.add('hidden');
}

async function saveNote() {
  const row = rows.find(r => r.symbol === noteSymbol);
  if (!row) return closeNote();
  const note = noteTextEl.value.trim();
  if ((row.note || '') === note) return closeNote();
  noteSaveEl.disabled = true;
  showNoteError('');
  try {
    const resp = await fetch('/api/note', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ symbol: row.symbol, note }),
    });
    if (!resp.ok) throw new Error((await resp.text()).trim());
    row.note = note;
    closeNote();
    render();
  } catch (err) {
    showNoteError(`Save failed: ${err.message}`);
    noteSaveEl.disabled = false;
  }
}

function matchesFilter(row) {
  if (zoneFilter === 'deep-demand') {
    if (!row.deepDemand) return false;
  } else if (zoneFilter !== 'all' && row.zone !== zoneFilter) return false;
  if (themeFilter === 'ai') {
    if (!row.aiTheme) return false;
  } else if (themeFilter === 'non-ai') {
    if (row.aiTheme) return false;
  } else if (themeFilter !== 'all' && row.aiTheme !== themeFilter) return false;
  if (sectorFilter !== 'all' && row.sector !== sectorFilter) return false;
  if (!query) return true;
  const q = query.toLowerCase();
  return [row.symbol, row.name, row.note, row.aiTheme, row.sector, row.industry].some(v => (v || '').toLowerCase().includes(q));
}

function sortRows(list) {
  const sorted = [...list];
  sorted.sort((a, b) => {
    const av = a[sortKey];
    const bv = b[sortKey];
    if (typeof av === 'string') return av.localeCompare(bv) * sortDir;
    return ((av ?? -Infinity) - (bv ?? -Infinity)) * sortDir;
  });
  return sorted;
}

function render() {
  const filtered = sortRows(rows.filter(matchesFilter));

  countEl.textContent = `${filtered.length} of ${rows.length} tickers`;
  emptyNoteEl.classList.toggle('hidden', filtered.length > 0);

  tableBodyEl.innerHTML = filtered.map(r => `
    <tr data-symbol="${r.symbol}">
      <td class="symbol"><a class="cell-link" href="chart.html?symbol=${encodeURIComponent(r.symbol)}">${r.symbol}</a></td>
      <td class="name">${r.name || ''}</td>
      <td>${r.aiTheme ? `<span class="theme-pill">${escapeHtml(r.aiTheme)}</span>` : '–'}</td>
      <td class="sector">${escapeHtml(r.sector) || '–'}</td>
      <td class="industry">${escapeHtml(r.industry) || '–'}</td>
      <td>${fmt(r.price)}</td>
      <td>${zonePill(r.zone)}</td>
      <td>${fmt(r.pegRatio)}</td>
      <td class="${pctClass(r.roicPct)}">${fmtPct(r.roicPct)}</td>
      <td>${fmt(r.psRatio)}</td>
      <td>${fmt(r.peRatio)}</td>
      <td class="${pctClass(r.revenueGrowthPct)}">${fmtPct(r.revenueGrowthPct)}</td>
      <td class="${pctClass(r.netMarginPct)}">${fmtPct(r.netMarginPct)}</td>
      <td>${ratingLabel(r.recommendationKey)}</td>
      <td class="${r.upsidePct >= 0 ? 'pos' : 'neg'}">${r.upsidePct === undefined || r.upsidePct === null ? '–' : `${r.upsidePct >= 0 ? '+' : ''}${fmt(r.upsidePct)}%`}</td>
      <td class="${r.netOpenMarket ? pctClass(r.netOpenMarket) : ''}"><a class="cell-link" href="insider.html?symbol=${encodeURIComponent(r.symbol)}">${fmtNetUsd(r.netOpenMarket)}</a></td>
      <td>${flagBadges(r)}</td>
      <td class="note-cell">${noteCell(r)}</td>
    </tr>
  `).join('');

  document.querySelectorAll('thead th').forEach(th => {
    th.classList.toggle('sorted', th.dataset.key === sortKey);
  });
}

tableBodyEl.addEventListener('click', e => {
  if (e.target.closest('.note-btn')) openNote(e.target.closest('tr').dataset.symbol);
});

tableWrapEl.addEventListener('scroll', () => {
  tableWrapEl.classList.toggle('scrolled', tableWrapEl.scrollLeft > 4);
});

noteSaveEl.addEventListener('click', saveNote);
document.getElementById('note-cancel').addEventListener('click', closeNote);

noteModalEl.addEventListener('mousedown', e => {
  if (e.target === noteModalEl) closeNote();
});

noteModalEl.addEventListener('keydown', e => {
  if (e.key === 'Escape') closeNote();
  if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) saveNote();
});

document.querySelectorAll('thead th').forEach(th => {
  th.addEventListener('click', () => {
    const key = th.dataset.key;
    if (!key) return;
    if (sortKey === key) {
      sortDir *= -1;
    } else {
      sortKey = key;
      sortDir = 1;
    }
    render();
  });
});

document.querySelectorAll('.filter-btn').forEach(btn => {
  btn.addEventListener('click', () => {
    zoneFilter = btn.dataset.zone;
    document.querySelectorAll('.filter-btn').forEach(b => b.classList.toggle('active', b === btn));
    render();
  });
});

themeFilterEl.addEventListener('change', () => {
  themeFilter = themeFilterEl.value;
  render();
});

sectorFilterEl.addEventListener('change', () => {
  sectorFilter = sectorFilterEl.value;
  render();
});

function fillFilterOptions(el, values, labelFor = v => v) {
  el.insertAdjacentHTML('beforeend', values.map(v => `<option value="${escapeHtml(v)}">${escapeHtml(labelFor(v))}</option>`).join(''));
}

searchEl.addEventListener('input', () => {
  query = searchEl.value.trim();
  render();
});

function carrySymbol() {
  const symbol = new URLSearchParams(window.location.search).get('symbol');
  if (!symbol) return;
  const enc = encodeURIComponent(symbol);
  document.getElementById('chart-link').href = `chart.html?symbol=${enc}`;
  document.getElementById('insider-link').href = `insider.html?symbol=${enc}`;
}

async function init() {
  carrySymbol();
  const resp = await fetch('/api/screener');
  if (!resp.ok) {
    emptyNoteEl.textContent = `Error: ${await resp.text()}`;
    emptyNoteEl.classList.remove('hidden');
    return;
  }
  const data = await resp.json();
  rows = data.rows || [];
  if (data.deepDemandPct !== undefined) {
    document.getElementById('deep-demand-btn').textContent = `Demand Low ${Number(data.deepDemandPct.toFixed(1))}%`;
  }
  const count = key => rows.filter(r => r[key]).length;
  fillFilterOptions(themeFilterEl, (data.aiThemes || []).filter(t => rows.some(r => r.aiTheme === t)));
  fillFilterOptions(sectorFilterEl, [...new Set(rows.map(r => r.sector).filter(Boolean))].sort());
  themeFilterEl.querySelector('[value="ai"]').textContent = `All AI (${count('aiTheme')})`;
  render();
}

init();
