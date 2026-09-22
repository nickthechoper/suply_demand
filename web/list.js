const tableBodyEl = document.getElementById('table-body');
const emptyNoteEl = document.getElementById('empty-note');
const searchEl = document.getElementById('search');
const countEl = document.getElementById('count');

let rows = [];
let zoneFilter = 'all';
let query = '';
let sortKey = 'symbol';
let sortDir = 1;

function fmt(n) {
  return n === undefined || n === null || Number.isNaN(n) ? '–' : n.toFixed(2);
}

function zonePill(zone) {
  const label = zone === 'supply' ? 'Supply' : zone === 'demand' ? 'Demand' : '—';
  return `<span class="zone-pill ${zone}">${label}</span>`;
}

function ratingLabel(key) {
  if (!key) return '–';
  return key.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

function scorePill(row) {
  if (!row.qualified) return `<span class="score-pill score-na" title="Missing P/E, PEG, revenue growth, margin, or analyst data">N/A</span>`;
  const score = row.score;
  const bucket = score >= 65 ? 'score-high' : score <= 35 ? 'score-low' : 'score-mid';
  return `<span class="score-pill ${bucket}">${Math.round(score)}</span>`;
}

function matchesFilter(row) {
  if (zoneFilter !== 'all' && row.zone !== zoneFilter) return false;
  if (!query) return true;
  const q = query.toLowerCase();
  return row.symbol.toLowerCase().includes(q) || row.name.toLowerCase().includes(q);
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
      <td class="symbol">${r.symbol}</td>
      <td class="name">${r.name || ''}</td>
      <td>${fmt(r.price)}</td>
      <td class="${r.changePct >= 0 ? 'pos' : 'neg'}">${r.changePct >= 0 ? '+' : ''}${fmt(r.changePct)}%</td>
      <td>${zonePill(r.zone)}</td>
      <td>${r.demandReached ? fmt(r.demandBtm) : '–'}</td>
      <td>${r.demandReached ? fmt(r.demandTop) : '–'}</td>
      <td>${r.supplyReached ? fmt(r.supplyBtm) : '–'}</td>
      <td>${r.supplyReached ? fmt(r.supplyTop) : '–'}</td>
      <td>${ratingLabel(r.recommendationKey)}</td>
      <td class="${r.upsidePct >= 0 ? 'pos' : 'neg'}">${r.upsidePct === undefined || r.upsidePct === null ? '–' : `${r.upsidePct >= 0 ? '+' : ''}${fmt(r.upsidePct)}%`}</td>
      <td>${scorePill(r)}</td>
    </tr>
  `).join('');

  document.querySelectorAll('thead th').forEach(th => {
    th.classList.toggle('sorted', th.dataset.key === sortKey);
  });
}

tableBodyEl.addEventListener('click', e => {
  const tr = e.target.closest('tr');
  if (!tr) return;
  window.location.href = `index.html?symbol=${encodeURIComponent(tr.dataset.symbol)}`;
});

document.querySelectorAll('thead th').forEach(th => {
  th.addEventListener('click', () => {
    const key = th.dataset.key;
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

searchEl.addEventListener('input', () => {
  query = searchEl.value.trim();
  render();
});

async function init() {
  const resp = await fetch('/api/screener');
  if (!resp.ok) {
    emptyNoteEl.textContent = `Error: ${await resp.text()}`;
    emptyNoteEl.classList.remove('hidden');
    return;
  }
  const data = await resp.json();
  rows = data.rows || [];
  sortKey = 'score';
  sortDir = -1;
  render();
}

init();
