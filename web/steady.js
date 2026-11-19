const MIN_RETURN = 100;
const MAX_DRAWDOWN = -30;
const TOGGLE_KEY = 'steady.only';

const rowsEl = document.getElementById('rows');
const emptyEl = document.getElementById('empty');
const summaryEl = document.getElementById('summary');
const toggleEl = document.getElementById('steady-toggle');
const headers = [...document.querySelectorAll('th[data-key]')];

let rows = [];
let sortKey = 'twoYearReturnPct';
let sortDir = -1;
let steadyOnly = loadSteadyOnly();

function loadSteadyOnly() {
  try {
    return localStorage.getItem(TOGGLE_KEY) !== '0';
  } catch {
    return true;
  }
}

function escapeHtml(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function signedPct(n) {
  return `${n >= 0 ? '+' : ''}${Math.round(n)}%`;
}

function isSteady(r) {
  return r.twoYearReturnPct >= MIN_RETURN && r.twoYearMaxDrawdownPct >= MAX_DRAWDOWN;
}

function render() {
  const eligible = rows.filter(r => r.twoYearReturnPct != null && r.twoYearMaxDrawdownPct != null);
  const shown = (steadyOnly ? eligible.filter(isSteady) : eligible).sort((a, b) => {
    if (sortKey === 'symbol') return a.symbol.localeCompare(b.symbol) * sortDir;
    return (a[sortKey] - b[sortKey]) * sortDir;
  });

  toggleEl.setAttribute('aria-pressed', String(steadyOnly));
  summaryEl.textContent = `${shown.length} of ${eligible.length} tickers`;
  emptyEl.classList.toggle('hidden', shown.length > 0);
  rowsEl.innerHTML = shown.map(r => `<tr>
    <td><a class="cell-link" href="chart.html?symbol=${encodeURIComponent(r.symbol)}"><span class="sym">${escapeHtml(r.symbol)}</span>${r.name ? ` <span class="nm">(${escapeHtml(r.name)})</span>` : ''}</a></td>
    <td class="num ${r.twoYearReturnPct >= 0 ? 'pos' : 'neg'}">${signedPct(r.twoYearReturnPct)}</td>
    <td class="num neg">${signedPct(r.twoYearMaxDrawdownPct)}</td>
  </tr>`).join('');

  for (const th of headers) {
    th.setAttribute('aria-sort', th.dataset.key === sortKey ? (sortDir > 0 ? 'ascending' : 'descending') : 'none');
  }
}

toggleEl.addEventListener('click', () => {
  steadyOnly = !steadyOnly;
  try {
    localStorage.setItem(TOGGLE_KEY, steadyOnly ? '1' : '0');
  } catch {}
  render();
});

for (const th of headers) {
  th.querySelector('button').addEventListener('click', () => {
    const key = th.dataset.key;
    sortDir = key === sortKey ? -sortDir : key === 'symbol' ? 1 : -1;
    sortKey = key;
    render();
  });
}

async function init() {
  const resp = await fetch('/api/screener');
  if (!resp.ok) {
    emptyEl.textContent = `Error: ${await resp.text()}`;
    emptyEl.classList.remove('hidden');
    return;
  }
  rows = (await resp.json()).rows || [];
  render();
}

init();
