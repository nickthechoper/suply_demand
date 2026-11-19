const tableWrapEl = document.getElementById('table-wrap');
const headRowEl = document.getElementById('head-row');
const tableBodyEl = document.getElementById('table-body');
const emptyNoteEl = document.getElementById('empty-note');
const searchEl = document.getElementById('search');
const themeFilterEl = document.getElementById('theme-filter');
const sectorFilterEl = document.getElementById('sector-filter');
const industryFilterEl = document.getElementById('industry-filter');
const countEl = document.getElementById('count');
const favFilterEl = document.getElementById('fav-filter');
const noteModalEl = document.getElementById('note-modal');
const noteTitleEl = document.getElementById('note-title');
const noteTextEl = document.getElementById('note-text');
const noteErrorEl = document.getElementById('note-error');
const noteHintEl = document.getElementById('note-hint');
const noteSaveEl = document.getElementById('note-save');

let rows = [];
let themeFilter = 'all';
let sectorFilter = 'all';
let industryFilter = 'all';
let query = '';
let favoritesOnly = loadFavoritesOnly();
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

function zonePill(row) {
  const label = row.zonePct === undefined || row.zonePct === null ? '—' : `${Number(row.zonePct.toFixed(1))}%`;
  const side = row.zonePct === undefined || row.zonePct === null ? 'none' : row.zonePct < 50 ? 'demand' : 'supply';
  return `<span class="zone-pill ${side}">${label}</span>`;
}

function signedPct(n) {
  return n === undefined || n === null || Number.isNaN(n) ? '–' : `${n >= 0 ? '+' : ''}${fmt(n)}%`;
}

function yieldChg(change, suffix, title) {
  if (change === undefined || change === null) return '';
  const bp = Math.round(change * 100);
  return `<span class="yield-chg ${bp > 0 ? 'neg' : bp < 0 ? 'pos' : ''}" title="${title}">${bp > 0 ? '+' : ''}${bp}bp${suffix}</span>`;
}

function bondYieldCell(row) {
  if (row.bondYieldPct === undefined || row.bondYieldPct === null) return '–';
  const day = yieldChg(row.bondYieldChange, '', 'Change since previous trade day');
  const week = yieldChg(row.bondYieldWeekChange, ' 1W', 'Change over the last week');
  return `<a class="cell-link" href="chart.html?symbol=${encodeURIComponent(row.symbol)}#bonds" title="${escapeHtml(row.bondLabel)}">${fmt(row.bondYieldPct)}%</a>${day}${week}`;
}

function bondYieldClass(y) {
  return y === undefined || y === null ? '' : y >= 15 ? 'yield-distress' : y >= 10 ? 'yield-high' : '';
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

function favButton(row) {
  const label = `${row.favorite ? 'Remove' : 'Add'} ${row.symbol} ${row.favorite ? 'from' : 'to'} favorites`;
  return `<button class="fav-btn" type="button" aria-pressed="${row.favorite ? 'true' : 'false'}" aria-label="${escapeHtml(label)}" title="${escapeHtml(label)}">${row.favorite ? '★' : '☆'}</button>`;
}

function filterButton(kind, value, active) {
  if (!value) return '–';
  const on = active === value;
  return `<button class="filter-link" type="button" data-filter="${kind}" data-value="${escapeHtml(value)}" aria-pressed="${on}" title="${on ? 'Clear' : 'Filter by'} ${kind}">${escapeHtml(value)}</button>`;
}

const FAV_ONLY_KEY = 'list.favoritesOnly';

function loadFavoritesOnly() {
  try {
    return localStorage.getItem(FAV_ONLY_KEY) === '1';
  } catch {
    return false;
  }
}

function setFavoritesOnly(on) {
  favoritesOnly = on;
  try {
    localStorage.setItem(FAV_ONLY_KEY, on ? '1' : '0');
  } catch {}
  render();
}

async function toggleFavorite(symbol) {
  const row = rows.find(r => r.symbol === symbol);
  if (!row) return;
  const favorite = !row.favorite;
  row.favorite = favorite;
  render();
  try {
    const resp = await fetch('/api/favorite', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ symbol, favorite }),
    });
    if (!resp.ok) throw new Error((await resp.text()).trim());
  } catch (err) {
    row.favorite = !favorite;
    render();
    alert(`Could not update favorite for ${symbol}: ${err.message}`);
  }
}

function applyCellFilter(kind, value) {
  if (kind === 'sector') {
    sectorFilter = sectorFilter === value ? 'all' : value;
    sectorFilterEl.value = sectorFilter;
    refreshIndustryOptions();
  } else {
    const row = rows.find(r => r.industry === value);
    if (industryFilter !== value && row && sectorFilter !== 'all' && sectorFilter !== row.sector) {
      sectorFilter = 'all';
      sectorFilterEl.value = sectorFilter;
    }
    industryFilter = industryFilter === value ? 'all' : value;
    refreshIndustryOptions();
  }
  render();
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
  if (favoritesOnly && !row.favorite) return false;
  if (themeFilter === 'ai') {
    if (!row.aiTheme) return false;
  } else if (themeFilter === 'non-ai') {
    if (row.aiTheme) return false;
  } else if (themeFilter !== 'all' && row.aiTheme !== themeFilter) return false;
  if (sectorFilter !== 'all' && row.sector !== sectorFilter) return false;
  if (industryFilter !== 'all' && row.industry !== industryFilter) return false;
  if (!query) return true;
  const q = query.toLowerCase();
  return [row.symbol, row.name, row.note, row.aiTheme, row.sector, row.industry].some(v => (v || '').toLowerCase().includes(q));
}

function matchRank(row) {
  const q = query.toLowerCase();
  const symbol = row.symbol.toLowerCase();
  const name = (row.name || '').toLowerCase();
  if (symbol === q) return 0;
  if (symbol.startsWith(q)) return 1;
  if (name.startsWith(q)) return 2;
  if (name.split(/[\s.,()-]+/).some(w => w.startsWith(q))) return 3;
  return 4;
}

function sortRows(list) {
  const sorted = [...list];
  sorted.sort((a, b) => {
    const av = a[sortKey];
    const bv = b[sortKey];
    if (typeof av === 'string') return av.localeCompare(bv) * sortDir;
    return ((av ?? -Infinity) - (bv ?? -Infinity)) * sortDir;
  });
  if (query) sorted.sort((a, b) => matchRank(a) - matchRank(b));
  return sorted;
}

const ORDER_KEY = 'list.columnOrder';

const COLUMNS = [
  { id: 'symbol', label: 'Symbol', left: true, tdClass: () => 'symbol', cell: r => `${favButton(r)}<a class="cell-link" href="chart.html?symbol=${encodeURIComponent(r.symbol)}">${r.symbol}</a>` },
  { id: 'name', label: 'Name', left: true, tdClass: () => 'name', cell: r => r.name || '' },
  { id: 'aiTheme', label: 'AI Theme', left: true, cell: r => r.aiTheme ? `<span class="theme-pill">${escapeHtml(r.aiTheme)}</span>` : '–' },
  { id: 'sector', label: 'Sector', left: true, tdClass: () => 'sector', cell: r => filterButton('sector', r.sector, sectorFilter) },
  { id: 'industry', label: 'Industry', left: true, tdClass: () => 'industry', cell: r => filterButton('industry', r.industry, industryFilter) },
  { id: 'price', label: 'Price', cell: r => fmt(r.price) },
  { id: 'dailyChangePct', label: 'Daily Change', tdClass: r => pctClass(r.dailyChangePct), cell: r => signedPct(r.dailyChangePct) },
  { id: 'weeklyChangePct', label: 'Weekly Change', title: 'Change vs the last close at least 7 days earlier', tdClass: r => pctClass(r.weeklyChangePct), cell: r => signedPct(r.weeklyChangePct) },
  { id: 'zonePct', label: 'Zone', cell: r => zonePill(r) },
  { id: 'pegRatio', label: 'PEG', cell: r => fmt(r.pegRatio) },
  { id: 'roicPct', label: 'ROIC TTM', tdClass: r => pctClass(r.roicPct), cell: r => fmtPct(r.roicPct) },
  { id: 'psRatio', label: 'P/S', cell: r => fmt(r.psRatio) },
  { id: 'peRatio', label: 'P/E', cell: r => fmt(r.peRatio) },
  { id: 'revenueGrowthPct', label: 'Revenue Growth (YoY)', tdClass: r => pctClass(r.revenueGrowthPct), cell: r => fmtPct(r.revenueGrowthPct) },
  { id: 'netMarginPct', label: 'Net Profit Margin', tdClass: r => pctClass(r.netMarginPct), cell: r => fmtPct(r.netMarginPct) },
  { id: 'recommendationKey', label: 'Rating', cell: r => ratingLabel(r.recommendationKey) },
  { id: 'upsidePct', label: 'Target Upside', tdClass: r => r.upsidePct >= 0 ? 'pos' : 'neg', cell: r => r.upsidePct === undefined || r.upsidePct === null ? '–' : `${r.upsidePct >= 0 ? '+' : ''}${fmt(r.upsidePct)}%` },
  { id: 'netOpenMarket', label: 'Net Open-Market (12M)', title: 'Open-market insider purchases minus sales over the last 12 months', tdClass: r => r.netOpenMarket ? pctClass(r.netOpenMarket) : '', cell: r => `<a class="cell-link" href="insider.html?symbol=${encodeURIComponent(r.symbol)}">${fmtNetUsd(r.netOpenMarket)}</a>` },
  { id: 'bondYieldPct', label: 'Bond Yield', title: "Highest yield to maturity among the company's straight (non-convertible) bonds traded in the last 14 days, from FINRA TRACE", tdClass: r => bondYieldClass(r.bondYieldPct), cell: r => bondYieldCell(r) },
  { id: 'flags', label: 'Flags', sortable: false, cell: r => flagBadges(r) },
  { id: 'note', label: 'Note', left: true, tdClass: () => 'note-cell', cell: r => noteCell(r) },
];

let columnOrder = loadColumnOrder();

function loadColumnOrder() {
  const ids = COLUMNS.map(c => c.id);
  let saved = [];
  try {
    saved = JSON.parse(localStorage.getItem(ORDER_KEY)) || [];
  } catch {}
  const order = saved.filter(id => ids.includes(id));
  ids.forEach(id => {
    if (!order.includes(id)) order.push(id);
  });
  return [...order.filter(id => id === 'symbol'), ...order.filter(id => id !== 'symbol')];
}

function orderedColumns() {
  return columnOrder.map(id => COLUMNS.find(c => c.id === id));
}

function saveColumnOrder() {
  try {
    localStorage.setItem(ORDER_KEY, JSON.stringify(columnOrder));
  } catch {}
}

function renderHead() {
  headRowEl.innerHTML = orderedColumns().map(c => {
    const pinned = c.id === 'symbol';
    return `<th data-id="${c.id}"${c.sortable === false ? '' : ` data-key="${c.id}"`}${c.title ? ` title="${escapeHtml(c.title)}"` : ''}${pinned ? '' : ' draggable="true"'}${c.left ? ' class="left"' : ''}>${c.label}</th>`;
  }).join('');
}

let dragId = null;

function clearDropMarks() {
  headRowEl.querySelectorAll('.drop-before, .drop-after').forEach(el => el.classList.remove('drop-before', 'drop-after'));
}

function dropSide(th, e) {
  const rect = th.getBoundingClientRect();
  return e.clientX < rect.left + rect.width / 2 ? 'before' : 'after';
}

headRowEl.addEventListener('dragstart', e => {
  const th = e.target.closest('th');
  if (!th || !th.draggable) return;
  dragId = th.dataset.id;
  e.dataTransfer.effectAllowed = 'move';
  e.dataTransfer.setData('text/plain', dragId);
  th.classList.add('dragging');
});

headRowEl.addEventListener('dragover', e => {
  const th = e.target.closest('th');
  if (!dragId || !th) return;
  e.preventDefault();
  clearDropMarks();
  if (th.dataset.id === 'symbol' || th.dataset.id === dragId) return;
  th.classList.add(`drop-${dropSide(th, e)}`);
});

headRowEl.addEventListener('drop', e => {
  const th = e.target.closest('th');
  if (!dragId || !th) return;
  e.preventDefault();
  const targetId = th.dataset.id;
  if (targetId !== 'symbol' && targetId !== dragId) {
    const side = dropSide(th, e);
    const order = columnOrder.filter(id => id !== dragId);
    const at = order.indexOf(targetId) + (side === 'after' ? 1 : 0);
    order.splice(at, 0, dragId);
    columnOrder = order;
    saveColumnOrder();
    renderHead();
    render();
  }
  dragId = null;
  clearDropMarks();
});

headRowEl.addEventListener('dragend', () => {
  dragId = null;
  clearDropMarks();
  headRowEl.querySelectorAll('.dragging').forEach(el => el.classList.remove('dragging'));
});

headRowEl.addEventListener('click', e => {
  const th = e.target.closest('th');
  const key = th && th.dataset.key;
  if (!key) return;
  if (sortKey === key) {
    sortDir *= -1;
  } else {
    sortKey = key;
    sortDir = 1;
  }
  render();
});

function render() {
  const filtered = sortRows(rows.filter(matchesFilter));

  const favCount = rows.filter(r => r.favorite).length;
  countEl.textContent = `${filtered.length} of ${rows.length} tickers`;
  favFilterEl.setAttribute('aria-pressed', favoritesOnly ? 'true' : 'false');
  favFilterEl.textContent = `★ Favorites (${favCount})`;
  emptyNoteEl.textContent = favoritesOnly && favCount === 0 ? 'No favorites yet. Click ☆ next to a ticker to add it.' : 'No tickers match.';
  emptyNoteEl.classList.toggle('hidden', filtered.length > 0);

  const cols = orderedColumns();
  tableBodyEl.innerHTML = filtered.map(r => `
    <tr data-symbol="${r.symbol}">${cols.map(c => `<td class="${c.tdClass ? c.tdClass(r) : ''}${c.left ? ' left' : ''}">${c.cell(r)}</td>`).join('')}</tr>
  `).join('');

  headRowEl.querySelectorAll('th').forEach(th => {
    th.classList.toggle('sorted', th.dataset.key === sortKey);
  });
}

tableBodyEl.addEventListener('click', e => {
  const symbol = e.target.closest('tr')?.dataset.symbol;
  if (e.target.closest('.note-btn')) return openNote(symbol);
  if (e.target.closest('.fav-btn')) return toggleFavorite(symbol);
  const filterBtn = e.target.closest('.filter-link');
  if (filterBtn) applyCellFilter(filterBtn.dataset.filter, filterBtn.dataset.value);
});

favFilterEl.addEventListener('click', () => setFavoritesOnly(!favoritesOnly));

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

themeFilterEl.addEventListener('change', () => {
  themeFilter = themeFilterEl.value;
  render();
});

sectorFilterEl.addEventListener('change', () => {
  sectorFilter = sectorFilterEl.value;
  refreshIndustryOptions();
  render();
});

industryFilterEl.addEventListener('change', () => {
  industryFilter = industryFilterEl.value;
  render();
});

function refreshIndustryOptions() {
  const industries = [...new Set(rows
    .filter(r => sectorFilter === 'all' || r.sector === sectorFilter)
    .map(r => r.industry)
    .filter(Boolean))].sort();
  if (industryFilter !== 'all' && !industries.includes(industryFilter)) industryFilter = 'all';
  industryFilterEl.innerHTML = '<option value="all">All industries</option>';
  fillFilterOptions(industryFilterEl, industries);
  industryFilterEl.value = industryFilter;
}

function fillFilterOptions(el, values, labelFor = v => v) {
  el.insertAdjacentHTML('beforeend', values.map(v => `<option value="${escapeHtml(v)}">${escapeHtml(labelFor(v))}</option>`).join(''));
}

searchEl.addEventListener('input', () => {
  query = searchEl.value.trim();
  render();
});

document.addEventListener('keydown', e => {
  if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === 'k') {
    e.preventDefault();
    if (!noteModalEl.classList.contains('hidden')) return;
    searchEl.focus();
    searchEl.select();
  }
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
  renderHead();
  const resp = await fetch('/api/screener');
  if (!resp.ok) {
    emptyNoteEl.textContent = `Error: ${await resp.text()}`;
    emptyNoteEl.classList.remove('hidden');
    return;
  }
  const data = await resp.json();
  rows = data.rows || [];
  const count = key => rows.filter(r => r[key]).length;
  fillFilterOptions(themeFilterEl, (data.aiThemes || []).filter(t => rows.some(r => r.aiTheme === t)));
  fillFilterOptions(sectorFilterEl, [...new Set(rows.map(r => r.sector).filter(Boolean))].sort());
  refreshIndustryOptions();
  themeFilterEl.querySelector('[value="ai"]').textContent = `All AI (${count('aiTheme')})`;
  render();
}

init();
