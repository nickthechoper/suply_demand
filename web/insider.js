const tickerEl = document.getElementById('ticker');
const periodEl = document.getElementById('period');
const fetchEl = document.getElementById('fetch');
const recentEl = document.getElementById('recent');
const resultEl = document.getElementById('result');
const chartLinkEl = document.getElementById('chart-link');
const listLinkEl = document.getElementById('list-link');

const TYPES = {
  P: { label: 'Purchase', kind: 'buy', color: '#26a69a' },
  S: { label: 'Sale', kind: 'sell', color: '#ef5350' },
  M: { label: 'Option exercise', kind: 'other', color: '#6f96ff' },
  X: { label: 'Option exercise', kind: 'other', color: '#6f96ff' },
  F: { label: 'Tax withholding', kind: 'other', color: '#e0c34a' },
  A: { label: 'Grant', kind: 'other', color: '#b39ddb' },
  G: { label: 'Gift', kind: 'other', color: '#9aa4af' },
  D: { label: 'Sale to issuer', kind: 'other', color: '#ff9c5c' },
  C: { label: 'Conversion', kind: 'other', color: '#80cbc4' },
  W: { label: 'Inherited', kind: 'other', color: '#9aa4af' },
};
const PERIODS = { 30: 'last 30 days', 90: 'last 3 months', 180: 'last 6 months', 365: 'last 12 months', 730: 'last 2 years' };
const COLUMNS = [
  { key: 'tradeDate', label: 'Date', left: true },
  { key: 'insider', label: 'Insider', left: true },
  { key: 'label', label: 'Transaction', left: true },
  { key: 'qty', label: 'Shares' },
  { key: 'price', label: 'Price' },
  { key: 'value', label: 'Value' },
  { key: 'owned', label: 'Holdings after' },
];
const PEOPLE_PREVIEW = 8;

let trackedNames = new Map();
let symbol = '';
let days = 365;
let data = null;
let filters = { labels: new Set(), insider: null, query: '', bar: null };
let sort = { key: 'tradeDate', dir: -1 };
let showAllPeople = false;
let chart = null;
let series = null;
let barTimes = [];
let closeByBar = new Map();
let requestId = 0;

function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function isNum(n) {
  return typeof n === 'number' && !Number.isNaN(n);
}

function fmtInt(n) {
  return isNum(n) ? Math.round(n).toLocaleString('en-US') : '–';
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

function fmtSignedShort(n) {
  return `${n > 0 ? '+' : n < 0 ? '−' : ''}${fmtShort(n)}`;
}

function toTime(date) {
  const [y, m, d] = date.split('-').map(Number);
  return Date.UTC(y, m - 1, d) / 1000;
}

function fmtDate(value) {
  const t = typeof value === 'number' ? value : toTime(value);
  return new Date(t * 1000).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric', timeZone: 'UTC' });
}

function daysAgo(date) {
  const n = Math.round((Date.now() / 1000 - toTime(date)) / 86400);
  return n <= 0 ? 'today' : n === 1 ? 'yesterday' : `${n} days ago`;
}

function timeAgo(unix) {
  const mins = Math.round((Date.now() / 1000 - unix) / 60);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.round(mins / 60);
  return hours < 24 ? `${hours}h ago` : `${Math.round(hours / 24)}d ago`;
}

function plural(n, word) {
  return `${n} ${word}${n === 1 ? '' : 's'}`;
}

function periodLabel() {
  return PERIODS[days] || `last ${days} days`;
}

function recentTickers() {
  try {
    return JSON.parse(localStorage.getItem('insider-recent') || '[]');
  } catch {
    return [];
  }
}

function rememberTicker(sym) {
  try {
    localStorage.setItem('insider-recent', JSON.stringify([sym, ...recentTickers().filter(s => s !== sym)].slice(0, 10)));
  } catch {}
}

function renderRecent() {
  const list = recentTickers();
  recentEl.innerHTML = list.length
    ? `<span class="label">Recent</span>${list.map(s => `<button type="button" data-symbol="${esc(s)}" ${s === symbol ? 'aria-current="true"' : ''}>${esc(s)}</button>`).join('')}`
    : '';
  recentEl.querySelectorAll('button').forEach(b => b.addEventListener('click', () => lookup(b.dataset.symbol)));
}

function renderPeriod() {
  periodEl.querySelectorAll('button').forEach(b => b.setAttribute('aria-pressed', String(Number(b.dataset.days) === days)));
}

async function loadTracked() {
  try {
    const resp = await fetch('/api/symbols');
    if (!resp.ok) return;
    const json = await resp.json();
    trackedNames = new Map(json.symbols.map(s => [s.symbol, s.name || '']));
    document.getElementById('tracked').innerHTML = json.symbols
      .map(s => `<option value="${esc(s.symbol)}">${esc(s.name || '')}</option>`)
      .join('');
    updateChartLink();
    if (!symbol) renderIntro();
  } catch (e) {
    console.error('loading tracked symbols failed', e);
  }
}

function updateChartLink() {
  chartLinkEl.href = trackedNames.has(symbol) ? `chart.html?symbol=${encodeURIComponent(symbol)}` : 'chart.html';
  listLinkEl.href = symbol ? `list.html?symbol=${encodeURIComponent(symbol)}` : 'list.html';
}

function suggestionButtons() {
  const picks = [...new Set([...recentTickers(), ...trackedNames.keys()])].slice(0, 8);
  return picks.map(s => `<button type="button" data-symbol="${esc(s)}">${esc(s)}</button>`).join('');
}

function bindSuggestions() {
  resultEl.querySelectorAll('.actions [data-symbol]').forEach(b => b.addEventListener('click', () => lookup(b.dataset.symbol)));
}

function renderIntro() {
  destroyChart();
  resultEl.innerHTML = `
    <div class="note">
      <h2>Look up insider activity for any ticker</h2>
      <p>SEC Form 4 filings from OpenInsider: who bought or sold on the open market, plotted against the share price. Press <b>/</b> to jump to the search box.</p>
      <div class="actions">${suggestionButtons()}</div>
    </div>`;
  bindSuggestions();
}

function renderLoading() {
  destroyChart();
  resultEl.innerHTML = `
    <div class="skeleton" aria-busy="true">
      <div class="status"><span class="spin"></span>Loading ${esc(symbol)} filings…${trackedNames.has(symbol) ? '' : ' this can take 10–20 seconds.'}</div>
      <div class="sk" style="height:38px;width:280px"></div>
      <div class="sk" style="height:58px"></div>
      <div class="sk-row">${'<div class="sk" style="height:84px"></div>'.repeat(4)}</div>
      <div class="sk" style="height:380px"></div>
    </div>`;
}

function renderError(message) {
  destroyChart();
  resultEl.innerHTML = `
    <div class="note err">
      <h2>Couldn't load ${esc(symbol)}</h2>
      <p>${esc(message)}</p>
      <div class="actions"><button type="button" id="retry">Try again</button></div>
    </div>`;
  document.getElementById('retry').addEventListener('click', load);
}

function lookup(sym) {
  tickerEl.value = sym;
  load();
}

async function load() {
  const sym = tickerEl.value.trim().toUpperCase().replace(/[^A-Z0-9.\-]/g, '');
  if (!sym) {
    tickerEl.focus();
    return;
  }
  symbol = sym;
  tickerEl.value = sym;
  const id = ++requestId;

  document.title = `${sym} Insider Trades — Supply & Demand Dashboard`;
  const url = new URL(window.location.href);
  url.searchParams.set('symbol', sym);
  if (days === 365) url.searchParams.delete('days'); else url.searchParams.set('days', String(days));
  history.replaceState(null, '', url);
  updateChartLink();
  renderRecent();

  fetchEl.disabled = true;
  renderLoading();
  try {
    const resp = await fetch(`/api/insider?symbol=${encodeURIComponent(sym)}&days=${days}`);
    if (id !== requestId) return;
    if (!resp.ok) throw new Error((await resp.text()).trim() || resp.statusText);
    const json = await resp.json();
    if (id !== requestId) return;
    data = prepare(json);
    filters = { labels: new Set(), insider: null, query: '', bar: null };
    sort = { key: 'tradeDate', dir: -1 };
    showAllPeople = false;
    if (data.trades.length || data.prices.length) rememberTicker(sym);
    renderRecent();
    renderResult();
  } catch (e) {
    if (id === requestId) renderError(e.message);
  } finally {
    if (id === requestId) fetchEl.disabled = false;
  }
}

function prepare(json) {
  const prices = (json.prices || []).map(p => ({ time: toTime(p.date), value: p.close }));
  barTimes = prices.map(p => p.time);
  closeByBar = new Map(prices.map(p => [p.time, p.value]));
  const trades = (json.trades || []).map((t, i) => {
    const meta = TYPES[t.code] || { label: t.type || t.code, kind: 'other', color: '#9aa4af' };
    const flags = t.flags || '';
    return { ...t, id: i, label: meta.label, kind: meta.kind, color: meta.color, suspect: flags.includes('E'), amended: flags.includes('A'), bar: snapToBar(toTime(t.tradeDate)) };
  });
  return { name: json.name || trackedNames.get(json.symbol) || '', trades, prices, fetchedAt: json.fetchedAt || 0 };
}

function snapToBar(t) {
  if (!barTimes.length) return null;
  let lo = 0, hi = barTimes.length;
  while (lo < hi) {
    const mid = (lo + hi) >> 1;
    if (barTimes[mid] < t) lo = mid + 1; else hi = mid;
  }
  const maxGap = 5 * 86400;
  if (lo < barTimes.length && barTimes[lo] - t <= maxGap) return barTimes[lo];
  if (lo === barTimes.length && t - barTimes[lo - 1] <= maxGap) return barTimes[lo - 1];
  return null;
}

function summarize(trades) {
  const buys = trades.filter(t => t.kind === 'buy');
  const sells = trades.filter(t => t.kind === 'sell');
  const total = list => list.reduce((s, t) => s + (t.value || 0), 0);
  const people = new Map();
  trades.forEach(t => {
    let p = people.get(t.insider);
    if (!p) {
      p = { name: t.insider, title: t.title, url: t.insiderUrl, count: 0, market: 0, last: t.tradeDate, owned: t.owned };
      people.set(t.insider, p);
    }
    p.count++;
    if (t.kind !== 'other') p.market += t.value || 0;
    if (t.tradeDate > p.last) {
      p.last = t.tradeDate;
      p.title = t.title;
      p.owned = t.owned;
    }
  });
  const ranked = [...people.values()].sort((a, b) => Math.abs(b.market) - Math.abs(a.market) || b.count - a.count || a.name.localeCompare(b.name));
  return {
    buys, sells,
    buyValue: total(buys),
    sellValue: total(sells),
    buyers: new Set(buys.map(t => t.insider)).size,
    sellers: new Set(sells.map(t => t.insider)).size,
    people: ranked,
    latest: trades.reduce((m, t) => (t.tradeDate > m ? t.tradeDate : m), ''),
  };
}

function flaggedNote(sum) {
  const n = [...sum.buys, ...sum.sells].filter(t => t.suspect).length;
  if (!n) return '';
  return ` <span class="warn">${n === 1 ? '1 of these trades is' : `${n} of these trades are`} flagged by OpenInsider as a possible filing error — check the SEC filing before relying on the totals.</span>`;
}

function verdict(sum, trades) {
  if (!trades.length) {
    const hint = data.prices.length ? '' : ' <span class="muted">No price history either — double-check the ticker symbol.</span>';
    return { cls: '', badge: 'No filings', text: `No Form 4 filings for ${esc(symbol)} in the ${periodLabel()}.${hint}` };
  }
  if (!sum.buys.length && !sum.sells.length) {
    const labels = [...new Set(trades.map(t => t.label.toLowerCase()))].slice(0, 3).join(', ');
    return {
      cls: '',
      badge: 'Routine only',
      text: `No open-market buys or sales. All ${trades.length} filings are ${esc(labels)} <span class="muted">— compensation activity, not a directional signal.</span>`,
    };
  }
  const net = sum.buyValue + sum.sellValue;
  if (net >= 0) {
    const top = sum.people.find(p => p.market > 0);
    return {
      cls: 'buy',
      badge: 'Net buying',
      text: `${plural(sum.buyers, 'insider')} bought <b>${fmtShort(sum.buyValue)}</b> on the open market${sum.sells.length ? `, against ${fmtShort(sum.sellValue)} sold by ${plural(sum.sellers, 'insider')}` : ', with no open-market sales'}.`
        + (top ? ` <span class="muted">Largest buyer: ${esc(top.name)} (${fmtShort(top.market)}).</span>` : '') + flaggedNote(sum),
    };
  }
  const top = sum.people.find(p => p.market < 0);
  return {
    cls: 'sell',
    badge: 'Net selling',
    text: `${plural(sum.sellers, 'insider')} sold <b>${fmtShort(sum.sellValue)}</b> on the open market${sum.buys.length ? `, against ${fmtShort(sum.buyValue)} bought by ${plural(sum.buyers, 'insider')}` : ', with no open-market purchases'}.`
      + (top ? ` <span class="muted">Largest seller: ${esc(top.name)} (${fmtShort(top.market)}).</span>` : '') + flaggedNote(sum),
  };
}

function renderResult() {
  const { trades } = data;
  const sum = summarize(trades);
  const v = verdict(sum, trades);
  const net = sum.buyValue + sum.sellValue;
  const enc = encodeURIComponent(symbol);
  const kpis = [
    ['Open-market buys', fmtShort(sum.buyValue), sum.buys.length ? `${plural(sum.buys.length, 'trade')} · ${plural(sum.buyers, 'insider')}` : 'none', sum.buyValue ? 'buy-text' : 'muted'],
    ['Open-market sales', fmtShort(sum.sellValue), sum.sells.length ? `${plural(sum.sells.length, 'trade')} · ${plural(sum.sellers, 'insider')}` : 'none', sum.sellValue ? 'sell-text' : 'muted'],
    ['Net open-market', net ? fmtSignedShort(net) : '$0', net > 0 ? 'net buying' : net < 0 ? 'net selling' : 'no open-market trades', net > 0 ? 'buy-text' : net < 0 ? 'sell-text' : 'muted'],
    ['All filings', trades.length.toLocaleString('en-US'), trades.length ? `${plural(sum.people.length, 'insider')} · latest ${daysAgo(sum.latest)}` : '—', ''],
  ];

  resultEl.innerHTML = `
    <div class="title-row">
      <h2>${esc(symbol)}</h2>
      ${data.name ? `<span class="company">${esc(data.name)}</span>` : ''}
      <span class="period">Insider trades · ${periodLabel()}</span>
      <nav class="links" aria-label="External sources">
        <a href="http://openinsider.com/${enc}" target="_blank" rel="noopener">OpenInsider ↗</a>
        <a href="https://www.sec.gov/cgi-bin/browse-edgar?action=getcompany&type=4&CIK=${enc}" target="_blank" rel="noopener">SEC EDGAR ↗</a>
        <a href="https://finance.yahoo.com/quote/${enc}" target="_blank" rel="noopener">Yahoo ↗</a>
      </nav>
    </div>

    <div class="verdict ${v.cls}"><span class="badge">${v.badge}</span><p>${v.text}</p></div>

    ${trades.length ? `<div class="kpis">${kpis.map(([k, val, n, cls]) => `
      <div class="kpi"><div class="k">${k}</div><div class="v num ${cls}">${val}</div><div class="n">${n}</div></div>`).join('')}
    </div>` : ''}

    ${trades.length || data.prices.length ? `<div class="split" ${trades.length ? '' : 'style="grid-template-columns:1fr"'}>
      <section class="card">
        <div class="card-head">
          <h3>Price &amp; insider activity</h3>
          ${trades.length ? '<span class="hint">Click a marker to see that day\'s filings</span>' : ''}
          ${trades.length ? `<div class="legend">
            <span><i class="tri-up"></i>Purchase</span>
            <span><i class="tri-down"></i>Sale</span>
            <span><i class="dot" style="background:#6f96ff"></i>Other filing</span>
          </div>` : ''}
        </div>
        <div class="chart-body">
          ${data.prices.length ? '<div id="chart"></div><div class="tooltip hidden" id="tooltip"></div>' : '<div class="chart-empty">Price history unavailable for this ticker.</div>'}
        </div>
      </section>
      ${trades.length ? `
      <section class="card">
        <div class="card-head"><h3>Who's trading</h3><span class="hint">Ranked by open-market value</span></div>
        <ul class="people" id="people"></ul>
        <div class="people-foot" id="people-foot"></div>
      </section>` : ''}
    </div>` : ''}

    ${trades.length ? `
    <section class="card" id="transactions">
      <div class="card-head"><h3>Transactions</h3><span class="count" id="count"></span></div>
      <div class="toolbar">
        <div id="chips" style="display:contents"></div>
        <div id="pills" style="display:contents"></div>
        <span class="grow"></span>
        <input id="search" type="search" placeholder="Search insider, title, type…" aria-label="Search transactions" autocomplete="off" spellcheck="false" />
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr>${COLUMNS.map(c => `<th class="${c.left ? 'l' : ''}" data-key="${c.key}"><button type="button">${c.label}</button></th>`).join('')}</tr></thead>
          <tbody id="table-body"></tbody>
        </table>
      </div>
    </section>` : ''}

    <p class="source">Source: OpenInsider (SEC Form 4), ${data.fetchedAt ? `refreshed in the background, last updated ${timeAgo(data.fetchedAt)}` : 'fetched on demand and cached for 10 minutes'}. Purchases and sales are open-market trades; option exercises, tax withholding, grants and gifts are routine compensation events. ⚠ marks filings OpenInsider flagged as containing an error.</p>
  `;

  if (data.prices.length) buildChart();
  if (!trades.length) return;

  data.summary = sum;
  renderChips();
  renderPeople();

  document.getElementById('search').addEventListener('input', e => {
    filters.query = e.target.value.trim().toLowerCase();
    apply();
  });
  resultEl.querySelectorAll('thead th').forEach(th => {
    th.querySelector('button').addEventListener('click', () => {
      const key = th.dataset.key;
      sort = { key, dir: sort.key === key ? -sort.dir : (COLUMNS.find(c => c.key === key).left && key !== 'tradeDate' ? 1 : -1) };
      renderTable();
    });
  });
  apply();
}

function renderChips() {
  const counts = new Map();
  data.trades.forEach(t => {
    const c = counts.get(t.label) || { n: 0, color: t.color };
    c.n++;
    counts.set(t.label, c);
  });
  const order = [...counts.entries()].sort((a, b) => b[1].n - a[1].n);
  const chips = document.getElementById('chips');
  chips.innerHTML = `
    <button type="button" class="chip" data-label="" aria-pressed="${filters.labels.size === 0}">All <small>${data.trades.length}</small></button>
    ${order.map(([label, c]) => `<button type="button" class="chip" data-label="${esc(label)}" aria-pressed="${filters.labels.has(label)}"><i class="dot" style="background:${c.color}"></i>${esc(label)} <small>${c.n}</small></button>`).join('')}`;
  chips.querySelectorAll('.chip').forEach(b => b.addEventListener('click', () => {
    const label = b.dataset.label;
    if (!label) filters.labels.clear();
    else if (filters.labels.has(label)) filters.labels.delete(label);
    else filters.labels.add(label);
    renderChips();
    apply();
  }));
}

function renderPeople() {
  const people = data.summary.people;
  const list = showAllPeople ? people : people.slice(0, PEOPLE_PREVIEW);
  const max = Math.max(...people.map(p => Math.abs(p.market)), 1);
  document.getElementById('people').innerHTML = list.map(p => {
    const pct = (Math.abs(p.market) / max) * 100;
    const cls = p.market > 0 ? 'buy-text' : p.market < 0 ? 'sell-text' : 'muted';
    return `<li><button type="button" data-name="${esc(p.name)}" aria-pressed="${filters.insider === p.name}" title="Show only ${esc(p.name)}'s filings">
      <span class="who">${esc(p.name)}</span>
      <span class="amt num ${cls}">${p.market ? fmtSignedShort(p.market) : '—'}</span>
      <span class="role">${esc(p.title || '—')}</span>
      <span class="meta">${plural(p.count, 'filing')} · ${esc(fmtDate(p.last).replace(/, \d{4}$/, ''))}</span>
      <span class="bar"><i style="width:${pct}%;background:${p.market >= 0 ? 'var(--buy)' : 'var(--sell)'}"></i></span>
    </button></li>`;
  }).join('');
  document.querySelectorAll('#people button').forEach(b => b.addEventListener('click', () => {
    filters.insider = filters.insider === b.dataset.name ? null : b.dataset.name;
    renderPeople();
    apply();
  }));
  const foot = document.getElementById('people-foot');
  foot.classList.toggle('hidden', people.length <= PEOPLE_PREVIEW);
  foot.innerHTML = `<button type="button" class="link-btn">${showAllPeople ? 'Show fewer' : `Show all ${people.length} insiders`}</button>`;
  foot.querySelector('button').addEventListener('click', () => {
    showAllPeople = !showAllPeople;
    renderPeople();
  });
}

function matches(t, ignoreBar = false) {
  if (filters.labels.size && !filters.labels.has(t.label)) return false;
  if (filters.insider && t.insider !== filters.insider) return false;
  if (filters.query && !`${t.insider} ${t.title} ${t.label} ${t.type}`.toLowerCase().includes(filters.query)) return false;
  if (!ignoreBar && filters.bar !== null && t.bar !== filters.bar) return false;
  return true;
}

function apply() {
  renderPills();
  renderTable();
  updateMarkers();
}

function renderPills() {
  const pills = [];
  if (filters.insider) pills.push(['insider', filters.insider]);
  if (filters.bar !== null) pills.push(['bar', `Traded ${fmtDate(filters.bar)}`]);
  const any = pills.length || filters.labels.size || filters.query;
  const el = document.getElementById('pills');
  el.innerHTML = pills.map(([k, text]) => `<span class="pill">${esc(text)}<button type="button" data-clear="${k}" aria-label="Remove filter">×</button></span>`).join('')
    + (any ? '<button type="button" class="link-btn" data-clear="all">Clear filters</button>' : '');
  el.querySelectorAll('[data-clear]').forEach(b => b.addEventListener('click', () => {
    const k = b.dataset.clear;
    if (k === 'insider' || k === 'all') filters.insider = null;
    if (k === 'bar' || k === 'all') filters.bar = null;
    if (k === 'all') {
      filters.labels.clear();
      filters.query = '';
      document.getElementById('search').value = '';
      renderChips();
    }
    renderPeople();
    apply();
  }));
}

function sortValue(t) {
  switch (sort.key) {
    case 'insider': return t.insider.toLowerCase();
    case 'label': return t.label;
    case 'tradeDate': return t.tradeDate + t.filingDate;
    default: return t[sort.key];
  }
}

function renderTable() {
  const visible = data.trades.filter(t => matches(t));
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
  document.getElementById('count').textContent = visible.length === data.trades.length
    ? plural(data.trades.length, 'filing')
    : `${visible.length} of ${plural(data.trades.length, 'filing')}`;

  document.getElementById('table-body').innerHTML = visible.length ? visible.map(t => {
    const lag = Math.round((toTime(t.filingDate) - toTime(t.tradeDate)) / 86400);
    const market = t.kind !== 'other';
    const qtyCls = market && isNum(t.qty) ? (t.qty > 0 ? 'pos' : t.qty < 0 ? 'neg' : '') : '';
    const date = esc(fmtDate(t.tradeDate));
    const name = esc(t.insider);
    return `<tr class="${market ? t.kind : 'routine'}" data-bar="${t.bar ?? ''}">
      <td class="l">
        <span class="primary">${t.filingUrl ? `<a class="ext" href="${esc(t.filingUrl)}" target="_blank" rel="noopener" title="Open SEC filing">${date}</a>` : date}</span>
        <span class="secondary">filed ${lag <= 0 ? 'same day' : `+${lag}d`}${t.suspect ? ' · <span class="warn" title="OpenInsider detected an error in this filing; verify the price and quantity on the SEC filing">⚠ possible error</span>' : t.amended ? ' · amended' : ''}</span>
      </td>
      <td class="l person">
        <span class="primary">${t.insiderUrl ? `<a class="ext" href="${esc(t.insiderUrl)}" target="_blank" rel="noopener" title="All trades by ${name}">${name}</a>` : name}</span>
        <span class="secondary" title="${esc(t.title)}">${esc(t.title || '—')}</span>
      </td>
      <td class="l"><span class="type" title="${esc(t.type)}"><i class="dot" style="background:${t.color}"></i>${esc(t.label)}</span></td>
      <td class="num fig ${qtyCls}">${fmtSignedInt(t.qty)}</td>
      <td class="num fig">${fmtUsd(t.price, 2)}</td>
      <td class="num fig ${market ? (t.value < 0 ? 'neg' : 'pos') : ''}">${fmtUsd(t.value)}</td>
      <td class="num">
        <span class="primary">${fmtInt(t.owned)}</span>
        <span class="secondary">${esc(t.ownedChange || '')}</span>
      </td>
    </tr>`;
  }).join('') : `<tr><td colspan="${COLUMNS.length}" class="empty">No filings match these filters.</td></tr>`;
}

function destroyChart() {
  if (chart) {
    chart.remove();
    chart = null;
    series = null;
  }
}

function buildChart() {
  destroyChart();
  const el = document.getElementById('chart');
  chart = LightweightCharts.createChart(el, {
    autoSize: true,
    layout: { background: { color: '#161b22' }, textColor: '#9aa4af', fontSize: 11 },
    grid: { vertLines: { color: 'rgba(42, 48, 56, 0.5)' }, horzLines: { color: 'rgba(42, 48, 56, 0.5)' } },
    rightPriceScale: { borderColor: '#2a3038', scaleMargins: { top: 0.12, bottom: 0.08 } },
    timeScale: { borderColor: '#2a3038', fixLeftEdge: true, fixRightEdge: true },
    crosshair: { mode: LightweightCharts.CrosshairMode.Magnet },
    handleScroll: { mouseWheel: false },
    handleScale: { mouseWheel: false },
  });
  series = chart.addAreaSeries({
    lineColor: '#6f96ff',
    topColor: 'rgba(111, 150, 255, 0.22)',
    bottomColor: 'rgba(111, 150, 255, 0.0)',
    lineWidth: 2,
    priceLineVisible: false,
  });
  series.setData(data.prices);
  chart.timeScale().fitContent();

  const tooltip = document.getElementById('tooltip');
  chart.subscribeCrosshairMove(param => {
    if (!param.time || !param.point || param.point.x < 0 || param.point.y < 0) {
      tooltip.classList.add('hidden');
      return;
    }
    const onDay = data.trades.filter(t => t.bar === param.time && matches(t, true));
    const close = closeByBar.get(param.time);
    const shown = onDay.slice(0, 6);
    tooltip.innerHTML = `
      <div class="tt-head"><span>${esc(fmtDate(param.time))}</span><b class="num">${fmtUsd(close, 2)}</b></div>
      ${onDay.length ? `<ul>${shown.map(t => `<li><i class="dot" style="background:${t.color}"></i><span class="who">${esc(t.insider)}</span><span class="num ${t.kind === 'buy' ? 'pos' : t.kind === 'sell' ? 'neg' : 'muted'}">${fmtSignedInt(t.qty)}</span></li>`).join('')}</ul>
      ${onDay.length > shown.length ? `<div class="more">+${onDay.length - shown.length} more</div>` : ''}
      <div class="cta">Click to filter the table</div>` : ''}`;
    tooltip.classList.remove('hidden');
    const box = el.getBoundingClientRect();
    const w = tooltip.offsetWidth;
    let x = param.point.x + 20;
    if (x + w > box.width) x = param.point.x - w - 12;
    tooltip.style.left = `${Math.max(8, x) + 8}px`;
    tooltip.style.top = '16px';
  });
  chart.subscribeClick(param => {
    if (!param.time || !data.trades.some(t => t.bar === param.time && matches(t, true))) return;
    filters.bar = filters.bar === param.time ? null : param.time;
    apply();
    if (filters.bar !== null) {
      const card = document.getElementById('transactions');
      card.scrollIntoView({ behavior: 'smooth', block: 'start' });
      card.querySelectorAll('tbody tr').forEach(tr => tr.classList.add('flash'));
    }
  });
}

function updateMarkers() {
  if (!series) return;
  const groups = new Map();
  data.trades.forEach(t => {
    if (t.bar === null || !matches(t, true)) return;
    const key = `${t.bar}|${t.kind === 'other' ? t.label : t.kind}`;
    const g = groups.get(key) || { t, n: 0 };
    g.n++;
    groups.set(key, g);
  });
  const markers = [...groups.values()].map(({ t, n }) => {
    const selected = filters.bar === t.bar;
    if (t.kind === 'buy') return { time: t.bar, position: 'belowBar', shape: 'arrowUp', color: t.color, size: selected ? 2 : 1.2, text: n > 1 ? String(n) : '' };
    if (t.kind === 'sell') return { time: t.bar, position: 'aboveBar', shape: 'arrowDown', color: t.color, size: selected ? 2 : 1.2, text: n > 1 ? String(n) : '' };
    return { time: t.bar, position: 'inBar', shape: 'circle', color: t.color, size: selected ? 1.2 : 0.6 };
  });
  markers.sort((a, b) => a.time - b.time);
  series.setMarkers(markers);
}

document.getElementById('lookup').addEventListener('submit', e => {
  e.preventDefault();
  load();
});
periodEl.querySelectorAll('button').forEach(b => b.addEventListener('click', () => {
  days = Number(b.dataset.days);
  renderPeriod();
  if (tickerEl.value.trim()) load();
}));
document.addEventListener('keydown', e => {
  if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
  const tag = document.activeElement?.tagName;
  if (tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA') return;
  e.preventDefault();
  tickerEl.focus();
  tickerEl.select();
});

const params = new URLSearchParams(window.location.search);
const requestedDays = Number(params.get('days'));
if (requestedDays > 0 && requestedDays <= 3650) days = requestedDays;
renderPeriod();
renderRecent();
loadTracked();
if (params.get('symbol')) lookup(params.get('symbol'));
else renderIntro();
