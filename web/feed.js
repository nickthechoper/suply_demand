const periodEl = document.getElementById('period');
const kindEl = document.getElementById('kind');
const minValueEl = document.getElementById('min-value');
const trackedEl = document.getElementById('tracked-only');
const searchEl = document.getElementById('search');
const resultEl = document.getElementById('result');
const updatedEl = document.getElementById('updated');

const TYPES = {
  P: { label: 'Purchase', kind: 'buy', color: '#26a69a' },
  S: { label: 'Sale', kind: 'sell', color: '#ef5350' },
};
const PERIODS = { 1: 'today', 3: 'last 3 days', 7: 'last week', 14: 'last 2 weeks', 30: 'last month' };
const COLUMNS = [
  { key: 'filed', label: 'Filed', left: true },
  { key: 'ticker', label: 'Company', left: true },
  { key: 'insider', label: 'Insider', left: true },
  { key: 'label', label: 'Type', left: true },
  { key: 'tradeDate', label: 'Traded', left: true },
  { key: 'qty', label: 'Shares' },
  { key: 'price', label: 'Price' },
  { key: 'value', label: 'Value' },
  { key: 'ownedChange', label: 'Δ Owned' },
];
const PAGE = 200;
const LEADERS = 8;
const REFRESH_MS = 5 * 60 * 1000;

let tracked = new Set();
let days = 3;
let kind = 'all';
let minValue = 0;
let trackedOnly = false;
let query = '';
let company = null;
let sort = { key: 'filed', dir: -1 };
let limit = PAGE;
let trades = [];
let fetchedAt = 0;
let requestId = 0;

function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function isNum(n) {
  return typeof n === 'number' && !Number.isNaN(n);
}

function fmtSignedInt(n) {
  if (!isNum(n)) return '–';
  return `${n > 0 ? '+' : n < 0 ? '−' : ''}${Math.abs(Math.round(n)).toLocaleString('en-US')}`;
}

function fmtUsd(n, digits = 0) {
  if (!isNum(n)) return '–';
  return `${n < 0 ? '−' : ''}$${Math.abs(n).toLocaleString('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits })}`;
}

function fmtShort(n) {
  const a = Math.abs(n);
  if (a >= 1e9) return `$${(a / 1e9).toFixed(2)}B`;
  if (a >= 1e6) return `$${(a / 1e6).toFixed(2)}M`;
  if (a >= 1e3) return `$${(a / 1e3).toFixed(1)}K`;
  return `$${a.toFixed(0)}`;
}

function plural(n, word, many = `${word}s`) {
  return `${n.toLocaleString('en-US')} ${n === 1 ? word : many}`;
}

function toTime(date) {
  const [y, m, d] = date.split('-').map(Number);
  return Date.UTC(y, m - 1, d) / 1000;
}

function fmtDate(date) {
  return new Date(toTime(date) * 1000).toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' });
}

function etNow() {
  const parts = Object.fromEntries(new Intl.DateTimeFormat('en-US', {
    timeZone: 'America/New_York', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
  }).formatToParts(new Date()).map(p => [p.type, p.value]));
  return Date.UTC(+parts.year, +parts.month - 1, +parts.day, +parts.hour, +parts.minute) / 1000;
}

function filedAgo(filed) {
  const m = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2})/.exec(filed || '');
  if (!m) return '';
  const mins = Math.round((etNow() - Date.UTC(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]) / 1000) / 60);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins} min ago`;
  if (mins < 24 * 60) return `${Math.round(mins / 60)}h ago`;
  const d = Math.round(mins / 1440);
  return d === 1 ? 'yesterday' : `${d} days ago`;
}

function timeAgo(unix) {
  const mins = Math.round((Date.now() / 1000 - unix) / 60);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins} min ago`;
  return `${Math.round(mins / 60)}h ago`;
}

function prepare(list) {
  return list.filter(t => TYPES[t.code]).map((t, i) => {
    const meta = TYPES[t.code];
    return { ...t, id: i, label: meta.label, kind: meta.kind, color: meta.color, abs: Math.abs(t.value || 0), suspect: (t.flags || '').includes('E') };
  });
}

function baseMatches(t) {
  if (minValue && t.abs < minValue) return false;
  if (trackedOnly && !tracked.has(t.ticker)) return false;
  if (query && !`${t.ticker} ${t.company} ${t.insider} ${t.title}`.toLowerCase().includes(query)) return false;
  return true;
}

function tableMatches(t) {
  if (kind !== 'all' && t.kind !== kind) return false;
  if (company && t.ticker !== company) return false;
  return true;
}

function byCompany(list) {
  const map = new Map();
  list.forEach(t => {
    let c = map.get(t.ticker);
    if (!c) {
      c = { ticker: t.ticker, company: t.company, buy: 0, sell: 0, buyers: new Set(), sellers: new Set(), last: '' };
      map.set(t.ticker, c);
    }
    if (t.kind === 'buy') {
      c.buy += t.abs;
      c.buyers.add(t.insider);
    } else {
      c.sell += t.abs;
      c.sellers.add(t.insider);
    }
    if ((t.filed || t.filingDate) > c.last) c.last = t.filed || t.filingDate;
  });
  return [...map.values()];
}

function renderLoading() {
  resultEl.innerHTML = `
    <div class="skeleton" aria-busy="true">
      <div class="status"><span class="spin"></span>Loading filings from the ${PERIODS[days]}…${days >= 14 ? ' this can take 10–20 seconds.' : ''}</div>
      <div class="sk-row">${'<div class="sk" style="height:84px"></div>'.repeat(4)}</div>
      <div class="sk" style="height:280px"></div>
      <div class="sk" style="height:420px"></div>
    </div>`;
}

function renderError(message) {
  resultEl.innerHTML = `
    <div class="note err">
      <h2>Couldn't load insider filings</h2>
      <p>${esc(message)}</p>
      <div class="actions"><button type="button" id="retry">Try again</button></div>
    </div>`;
  document.getElementById('retry').addEventListener('click', () => load());
}

async function load(silent = false) {
  const id = ++requestId;
  syncUrl();
  if (!silent) {
    renderLoading();
    updatedEl.textContent = '';
  }
  try {
    if (!silent && days > 3 && !trades.length) {
      const quick = await fetch('/api/insider/latest?days=1');
      if (id !== requestId) return;
      if (quick.ok) {
        const quickJson = await quick.json();
        if (id !== requestId) return;
        trades = prepare(quickJson.trades || []);
        fetchedAt = quickJson.fetchedAt || 0;
        limit = PAGE;
        render();
      }
    }
    const resp = await fetch(`/api/insider/latest?days=${days}`);
    if (id !== requestId) return;
    if (!resp.ok) throw new Error((await resp.text()).trim() || resp.statusText);
    const json = await resp.json();
    if (id !== requestId) return;
    trades = prepare(json.trades || []);
    fetchedAt = json.fetchedAt || 0;
    if (!silent) limit = PAGE;
    render();
  } catch (e) {
    if (id === requestId && !silent) renderError(e.message);
  }
}

async function loadTracked() {
  try {
    const resp = await fetch('/api/symbols');
    if (!resp.ok) return;
    const json = await resp.json();
    tracked = new Set(json.symbols.map(s => s.symbol));
    if (trades.length) render();
  } catch (e) {
    console.error('loading tracked symbols failed', e);
  }
}

function syncUrl() {
  const url = new URL(window.location.href);
  if (days === 3) url.searchParams.delete('days'); else url.searchParams.set('days', String(days));
  if (kind === 'all') url.searchParams.delete('kind'); else url.searchParams.set('kind', kind);
  history.replaceState(null, '', url);
}

function renderControls() {
  periodEl.querySelectorAll('button').forEach(b => b.setAttribute('aria-pressed', String(Number(b.dataset.days) === days)));
  kindEl.querySelectorAll('button').forEach(b => b.setAttribute('aria-pressed', String(b.dataset.kind === kind)));
  trackedEl.setAttribute('aria-pressed', String(trackedOnly));
  trackedEl.disabled = !tracked.size;
  trackedEl.title = tracked.size ? `Only the ${tracked.size} tickers on your list` : 'No tracked tickers yet';
}

function render() {
  renderControls();
  updatedEl.textContent = fetchedAt ? `Updated ${timeAgo(fetchedAt)}` : '';
  if (!trades.length) {
    resultEl.innerHTML = `<div class="note"><h2>No open-market trades filed ${PERIODS[days]}</h2><p>Try a longer period — filings slow down on weekends and holidays.</p></div>`;
    return;
  }

  const base = trades.filter(baseMatches);
  const companies = byCompany(base);
  const buys = base.filter(t => t.kind === 'buy');
  const sells = base.filter(t => t.kind === 'sell');
  const buyValue = buys.reduce((s, t) => s + t.abs, 0);
  const sellValue = sells.reduce((s, t) => s + t.abs, 0);
  const buyCos = companies.filter(c => c.buy > 0);
  const sellCos = companies.filter(c => c.sell > 0);
  const clusters = buyCos.filter(c => c.buyers.size >= 2).sort((a, b) => b.buyers.size - a.buyers.size || b.buy - a.buy);
  const share = buyValue + sellValue ? (buyValue / (buyValue + sellValue)) * 100 : 0;

  resultEl.innerHTML = `
    <div class="kpis">
      <div class="kpi"><div class="k">Insider buying</div><div class="v num ${buyValue ? 'buy-text' : 'muted'}">${fmtShort(buyValue)}</div><div class="n">${buys.length ? `${plural(buys.length, 'trade')} · ${plural(buyCos.length, 'company', 'companies')}` : 'none'}</div></div>
      <div class="kpi"><div class="k">Insider selling</div><div class="v num ${sellValue ? 'sell-text' : 'muted'}">${fmtShort(sellValue)}</div><div class="n">${sells.length ? `${plural(sells.length, 'trade')} · ${plural(sellCos.length, 'company', 'companies')}` : 'none'}</div></div>
      <div class="kpi"><div class="k">Buy share of value</div><div class="v num">${buyValue + sellValue ? `${share.toFixed(share < 10 ? 1 : 0)}%` : '–'}</div>
        <div class="ratio" aria-hidden="true"><i style="width:${share}%;background:var(--buy)"></i><i style="width:${100 - share}%;background:var(--sell)"></i></div></div>
      <div class="kpi"><div class="k">Cluster buys</div><div class="v num ${clusters.length ? 'buy-text' : 'muted'}">${clusters.length}</div><div class="n" title="Companies where two or more insiders bought">${clusters.length ? esc(clusters.slice(0, 4).map(c => c.ticker).join(', ')) + (clusters.length > 4 ? '…' : '') : '2+ insiders buying the same stock'}</div></div>
    </div>

    <div class="leaders">
      <section class="card">
        <div class="card-head"><h3>Most bought</h3><span class="hint">Ranked by purchase value</span></div>
        <ul class="ranked" id="most-bought"></ul>
      </section>
      <section class="card">
        <div class="card-head"><h3>Most sold</h3><span class="hint">Ranked by sale value</span></div>
        <ul class="ranked" id="most-sold"></ul>
      </section>
    </div>

    <section class="card">
      <div class="card-head"><h3>All trades</h3><span class="count" id="count"></span></div>
      <div class="toolbar" id="pills"></div>
      <div class="table-wrap">
        <table>
          <thead><tr>${COLUMNS.map(c => `<th class="${c.left ? 'l' : ''}" data-key="${c.key}"><button type="button">${c.label}</button></th>`).join('')}</tr></thead>
          <tbody id="table-body"></tbody>
        </table>
      </div>
      <div class="more-row hidden" id="more"></div>
    </section>

    <p class="source">Source: OpenInsider (SEC Form 4), cached for 10 minutes. Shows open-market purchases (P) and sales (S) only; option exercises, grants and tax withholding are left out. Filing times are US Eastern.</p>`;

  renderLeaders('most-bought', [...buyCos].sort((a, b) => b.buy - a.buy), 'buy');
  renderLeaders('most-sold', [...sellCos].sort((a, b) => b.sell - a.sell), 'sell');
  resultEl.querySelectorAll('thead th').forEach(th => th.querySelector('button').addEventListener('click', () => {
    const key = th.dataset.key;
    const col = COLUMNS.find(c => c.key === key);
    sort = { key, dir: sort.key === key ? -sort.dir : (col.left && !['filed', 'tradeDate'].includes(key) ? 1 : -1) };
    renderTable();
  }));
  renderTable();
}

function renderLeaders(id, list, side) {
  const el = document.getElementById(id);
  const top = list.slice(0, LEADERS);
  if (!top.length) {
    el.innerHTML = `<li class="empty">No open-market ${side === 'buy' ? 'purchases' : 'sales'} match these filters.</li>`;
    return;
  }
  const max = Math.max(...top.map(c => c[side]), 1);
  el.innerHTML = top.map(c => {
    const people = side === 'buy' ? c.buyers : c.sellers;
    return `<li><button type="button" data-ticker="${esc(c.ticker)}" aria-pressed="${company === c.ticker}" title="Show only ${esc(c.ticker)} trades">
      <span class="sym">${esc(c.ticker)}</span>
      <span class="co">${esc(c.company)}</span>
      <span class="amt num ${side === 'buy' ? 'buy-text' : 'sell-text'}">${fmtShort(c[side])}</span>
      <span class="meta">${plural(people.size, 'insider')}${people.size >= 2 ? ` <span class="tag cluster ${side}">Cluster</span>` : ''}${tracked.has(c.ticker) ? ' <span class="tag tracked">Tracked</span>' : ''}<span class="bar"><i style="width:${(c[side] / max) * 100}%;background:var(--${side})"></i></span></span>
    </button></li>`;
  }).join('');
  el.querySelectorAll('button[data-ticker]').forEach(b => b.addEventListener('click', () => {
    company = company === b.dataset.ticker ? null : b.dataset.ticker;
    limit = PAGE;
    render();
    if (company) document.getElementById('table-body').closest('.card').scrollIntoView({ behavior: 'smooth', block: 'start' });
  }));
}

function sortValue(t) {
  switch (sort.key) {
    case 'filed': return t.filed || t.filingDate;
    case 'ticker': return t.ticker;
    case 'insider': return t.insider.toLowerCase();
    case 'value': return t.abs;
    case 'qty': return isNum(t.qty) ? Math.abs(t.qty) : null;
    case 'ownedChange': {
      const n = parseFloat(String(t.ownedChange).replace(/[+%,]/g, ''));
      return Number.isNaN(n) ? (/new/i.test(t.ownedChange) ? Infinity : null) : Math.abs(n);
    }
    default: return t[sort.key];
  }
}

function renderPills() {
  const el = document.getElementById('pills');
  const pills = [];
  if (company) pills.push(['company', company]);
  if (kind !== 'all') pills.push(['kind', kind === 'buy' ? 'Buys only' : 'Sales only']);
  if (minValue) pills.push(['min', `${fmtShort(minValue)}+`]);
  if (trackedOnly) pills.push(['tracked', 'My tickers']);
  if (query) pills.push(['query', `“${query}”`]);
  el.classList.toggle('hidden', !pills.length);
  el.innerHTML = pills.map(([k, text]) => `<span class="pill">${esc(text)}<button type="button" data-clear="${k}" aria-label="Remove filter">×</button></span>`).join('')
    + (pills.length > 1 ? '<button type="button" class="link-btn" data-clear="all">Clear all</button>' : '');
  el.querySelectorAll('[data-clear]').forEach(b => b.addEventListener('click', () => {
    const k = b.dataset.clear;
    if (k === 'company' || k === 'all') company = null;
    if (k === 'kind' || k === 'all') kind = 'all';
    if (k === 'min' || k === 'all') minValue = 0;
    if (k === 'tracked' || k === 'all') trackedOnly = false;
    if (k === 'query' || k === 'all') query = '';
    minValueEl.value = String(minValue);
    searchEl.value = query;
    syncUrl();
    render();
  }));
}

function renderTable() {
  renderPills();
  const visible = trades.filter(t => baseMatches(t) && tableMatches(t));
  visible.sort((a, b) => {
    const x = sortValue(a), y = sortValue(b);
    if (x === y) return 0;
    if (x === null || x === undefined) return 1;
    if (y === null || y === undefined) return -1;
    return (x < y ? -1 : 1) * sort.dir;
  });
  resultEl.querySelectorAll('thead th').forEach(th => {
    if (th.dataset.key === sort.key) th.setAttribute('aria-sort', sort.dir === 1 ? 'ascending' : 'descending');
    else th.removeAttribute('aria-sort');
  });
  document.getElementById('count').textContent = visible.length === trades.length
    ? plural(trades.length, 'trade')
    : `${visible.length.toLocaleString('en-US')} of ${plural(trades.length, 'trade')}`;

  const rows = visible.slice(0, limit);
  document.getElementById('table-body').innerHTML = rows.length ? rows.map(t => {
    const date = t.filed ? t.filed.slice(11, 16) : '';
    const filedLabel = esc(`${fmtDate(t.filingDate)}${date ? `, ${date}` : ''}`);
    const name = esc(t.insider);
    const sym = encodeURIComponent(t.ticker);
    return `<tr class="${t.kind}">
      <td class="l">
        <span class="primary">${t.filingUrl ? `<a class="ext" href="${esc(t.filingUrl)}" target="_blank" rel="noopener" title="Open SEC filing">${esc(filedAgo(t.filed) || fmtDate(t.filingDate))}</a>` : esc(filedAgo(t.filed) || fmtDate(t.filingDate))}</span>
        <span class="secondary">${filedLabel}</span>
      </td>
      <td class="l company">
        <span class="primary"><a class="sym" href="insider.html?symbol=${sym}" title="All ${esc(t.ticker)} insider trades">${esc(t.ticker)}</a>${tracked.has(t.ticker) ? ' <span class="tag tracked">Tracked</span>' : ''}</span>
        <span class="secondary" title="${esc(t.company)}">${esc(t.company)}</span>
      </td>
      <td class="l person">
        <span class="primary">${t.insiderUrl ? `<a class="ext" href="${esc(t.insiderUrl)}" target="_blank" rel="noopener" title="All trades by ${name}">${name}</a>` : name}</span>
        <span class="secondary" title="${esc(t.title)}">${esc(t.title || '—')}</span>
      </td>
      <td class="l"><span class="type"><i class="dot" style="background:${t.color}"></i>${t.label}${t.suspect ? ' <span class="warn" title="OpenInsider flagged a possible error in this filing">⚠</span>' : ''}</span></td>
      <td class="l">${esc(fmtDate(t.tradeDate))}</td>
      <td class="num ${t.kind === 'buy' ? 'pos' : 'neg'}">${fmtSignedInt(t.qty)}</td>
      <td class="num">${fmtUsd(t.price, 2)}</td>
      <td class="num ${t.kind === 'buy' ? 'pos' : 'neg'}"><b>${fmtUsd(t.value)}</b></td>
      <td class="num muted">${esc(t.ownedChange || '–')}</td>
    </tr>`;
  }).join('') : `<tr><td colspan="${COLUMNS.length}" class="empty">No trades match these filters.</td></tr>`;

  const more = document.getElementById('more');
  more.classList.toggle('hidden', visible.length <= limit);
  more.innerHTML = `<button type="button">Show ${Math.min(PAGE, visible.length - limit).toLocaleString('en-US')} more</button>`;
  more.querySelector('button').addEventListener('click', () => {
    limit += PAGE;
    renderTable();
  });
}

periodEl.querySelectorAll('button').forEach(b => b.addEventListener('click', () => {
  if (Number(b.dataset.days) === days) return;
  days = Number(b.dataset.days);
  company = null;
  renderControls();
  load();
}));
kindEl.querySelectorAll('button').forEach(b => b.addEventListener('click', () => {
  kind = b.dataset.kind;
  limit = PAGE;
  syncUrl();
  if (trades.length) render(); else renderControls();
}));
minValueEl.addEventListener('change', () => {
  minValue = Number(minValueEl.value);
  limit = PAGE;
  if (trades.length) render();
});
trackedEl.addEventListener('click', () => {
  trackedOnly = !trackedOnly;
  limit = PAGE;
  if (trades.length) render(); else renderControls();
});
let searchTimer = 0;
searchEl.addEventListener('input', () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    query = searchEl.value.trim().toLowerCase();
    limit = PAGE;
    if (trades.length) render();
  }, 120);
});
document.addEventListener('keydown', e => {
  const tag = document.activeElement?.tagName;
  if (e.key === 'Escape' && document.activeElement === searchEl && searchEl.value) {
    searchEl.value = '';
    searchEl.dispatchEvent(new Event('input'));
    return;
  }
  if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
  if (tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA') return;
  e.preventDefault();
  searchEl.focus();
  searchEl.select();
});
setInterval(() => {
  if (document.visibilityState === 'visible') load(true);
}, REFRESH_MS);

const params = new URLSearchParams(window.location.search);
if (PERIODS[Number(params.get('days'))]) days = Number(params.get('days'));
if (['buy', 'sell'].includes(params.get('kind'))) kind = params.get('kind');
renderControls();
loadTracked();
load();
