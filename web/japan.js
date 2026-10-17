const resultEl = document.getElementById('result');
const periodEl = document.getElementById('period');

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const RANGES = [
  { label: '1Y', months: 12 },
  { label: '3Y', months: 36 },
  { label: '5Y', months: 60 },
  { label: 'All', months: 0 },
];
const VIEWS = [
  { kind: 'country', label: 'Countries' },
  { kind: 'region', label: 'Regions' },
  { kind: 'group', label: 'Groups' },
];
const PREFS_KEY = 'japan.prefs';
const HIDE_SMALLEST = 10;

let flows = null;
let range = 12;
let view = 'country';
let query = '';
let sort = { key: 'net', dir: -1 };
let showSmall = false;

function loadPrefs() {
  try {
    const p = JSON.parse(localStorage.getItem(PREFS_KEY)) || {};
    if (RANGES.some((r) => r.months === p.range)) range = p.range;
    if (VIEWS.some((v) => v.kind === p.view)) view = p.view;
    if (p.sort && typeof p.sort.key === 'string' && !p.sort.key.startsWith('m:')) sort = p.sort;
  } catch {}
}

function savePrefs() {
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify({ range, view, sort: sort.key.startsWith('m:') ? { key: 'net', dir: -1 } : sort }));
  } catch {}
}

function monthLabel(ym, long) {
  const [y, m] = ym.split('-');
  return long ? `${MONTHS[+m - 1]} ${y}` : `${MONTHS[+m - 1]} ${y.slice(2)}`;
}

function num(v) {
  if (v == null) return '·';
  const a = Math.abs(v);
  const digits = a >= 100 ? 0 : 1;
  const s = a.toLocaleString('en-US', { minimumFractionDigits: digits, maximumFractionDigits: digits });
  return v > 0 ? `+${s}` : v < 0 ? `−${s}` : '0';
}

function money(v) {
  if (v == null) return '—';
  const a = Math.abs(v);
  const s = a >= 1000 ? `¥${(a / 1000).toFixed(1)}tn` : `¥${a.toFixed(0)}bn`;
  return `${v < 0 ? '−' : '+'}${s}`;
}

function signClass(v) {
  return v > 0 ? 'pos' : v < 0 ? 'neg' : '';
}

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function startIndex() {
  return range > 0 ? Math.max(0, flows.months.length - range) : 0;
}

function rangeLabel() {
  return range > 0 ? RANGES.find((r) => r.months === range).label : `since ${monthLabel(flows.months[0])}`;
}

function sumOf(values) {
  const present = values.filter((v) => v != null);
  return present.length ? { total: Math.round(present.reduce((a, b) => a + b, 0) * 10) / 10, count: present.length } : null;
}

function stats(row) {
  const end = flows.months.length;
  const inRange = sumOf(row.values.slice(startIndex(), end));
  const last3 = sumOf(row.values.slice(Math.max(0, end - 3), end));
  const avg = inRange ? inRange.total / inRange.count : null;
  return {
    activity: row.values.slice(startIndex(), end).reduce((a, v) => a + Math.abs(v ?? 0), 0),
    net: inRange ? inRange.total : null,
    avg,
    last3: last3 ? last3.total : null,
    pace: last3 && avg != null ? last3.total / last3.count - avg : null,
    buying: inRange ? row.values.slice(startIndex(), end).filter((v) => v > 0).length : null,
    months: inRange ? inRange.count : 0,
  };
}

async function load() {
  loadPrefs();
  try {
    const res = await fetch('/api/japan-flows');
    if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
    flows = await res.json();
    renderShell();
    update();
  } catch (err) {
    resultEl.innerHTML = `
      <div class="note err">
        <h2>Couldn't load the data</h2>
        <p>${esc(err.message)}</p>
      </div>`;
  }
}

function renderShell() {
  const n = flows.months.length;
  periodEl.textContent = `Net purchases of foreign long-term bonds by Japanese investors · monthly, ¥bn · ${monthLabel(flows.months[0], true)} – ${monthLabel(flows.months[n - 1], true)}`;

  resultEl.innerHTML = `
    <div class="kpis" id="kpis"></div>
    <div class="card">
      <div class="card-head">
        <h3>Net purchases by market</h3>
        <span class="hint">¥bn · positive = Japan bought more than it sold</span>
        <div class="tools">
          <input id="filter" type="search" placeholder="Filter…" autocomplete="off" spellcheck="false" aria-label="Filter markets" />
          <div class="segmented" id="view" role="group" aria-label="Rows">
            ${VIEWS.map((v) => `<button type="button" data-kind="${v.kind}" aria-pressed="${v.kind === view}">${v.label}</button>`).join('')}
          </div>
          <div class="segmented" id="range" role="group" aria-label="Time range">
            ${RANGES.map((r) => `<button type="button" data-months="${r.months}" aria-pressed="${r.months === range}">${r.label}</button>`).join('')}
          </div>
        </div>
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr id="head"></tr></thead>
          <tbody id="body"></tbody>
        </table>
        <div id="empty" class="hidden">No markets match.</div>
      </div>
      <div class="table-foot">
        <span id="small-note"></span>
        <span><i class="swatch pos"></i>bought</span>
        <span><i class="swatch neg"></i>sold</span>
        <span>Month shading is relative to each row's largest month in view.</span>
        <span>3M vs avg = average of the last 3 months minus the average month in the period; positive means buying is picking up or selling is easing.</span>
      </div>
    </div>
    <p class="source">
      Source: Bank of Japan / Ministry of Finance balance of payments (BPM6), portfolio investment assets, long-term debt securities, net, by country of issuer.
      Euro area (12) is the sum of the twelve euro-area members BOJ reports separately. Blank months (·) are not published by BOJ.
      Fetched ${new Date(flows.fetchedAt * 1000).toLocaleString()}.
    </p>`;

  document.getElementById('range').addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    range = +b.dataset.months;
    document.querySelectorAll('#range button').forEach((x) => x.setAttribute('aria-pressed', String(x === b)));
    if (sort.key.startsWith('m:') && +sort.key.slice(2) < startIndex()) sort = { key: 'net', dir: -1 };
    savePrefs();
    update();
  });
  document.getElementById('view').addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    view = b.dataset.kind;
    document.querySelectorAll('#view button').forEach((x) => x.setAttribute('aria-pressed', String(x === b)));
    savePrefs();
    update();
  });
  document.getElementById('filter').addEventListener('input', (e) => {
    query = e.target.value.trim().toLowerCase();
    update();
  });
  document.getElementById('small-note').addEventListener('click', (e) => {
    if (!e.target.closest('button')) return;
    showSmall = !showSmall;
    update();
  });
  document.getElementById('head').addEventListener('click', (e) => {
    const th = e.target.closest('th');
    if (!th || !th.dataset.key) return;
    const key = th.dataset.key;
    sort = sort.key === key ? { key, dir: -sort.dir } : { key, dir: key === 'name' ? 1 : -1 };
    savePrefs();
    update();
  });
}

function kpi(label, value, note) {
  return `
    <div class="kpi">
      <div class="k">${esc(label)}</div>
      <div class="v num ${signClass(value)}">${money(value)}</div>
      <div class="n">${note}</div>
    </div>`;
}

function renderKpis() {
  const end = flows.months.length;
  const total = flows.rows.find((r) => r.kind === 'total');
  const countries = flows.rows.filter((r) => r.kind === 'country' && r.stats.net != null);
  const byNet = [...countries].sort((a, b) => b.stats.net - a.stats.net);
  const top = byNet[0];
  const bottom = byNet[byNet.length - 1];
  const period = `${rangeLabel()} to ${monthLabel(flows.months[end - 1], true)}`;
  document.getElementById('kpis').innerHTML = [
    total && kpi(`All countries · net ${rangeLabel()}`, total.stats.net, `${period} · latest month ${money(total.values[end - 1])}`),
    top && kpi(`Most bought · ${top.name}`, top.stats.net, `${period} · last 3 months ${money(top.stats.last3)}`),
    bottom && kpi(`Most sold · ${bottom.name}`, bottom.stats.net, `${period} · last 3 months ${money(bottom.stats.last3)}`),
  ].filter(Boolean).join('');
}

function sortValue(row, key) {
  if (key === 'name') return row.name;
  if (key.startsWith('m:')) return row.values[+key.slice(2)];
  return row.stats[key];
}

function compare(a, b) {
  const av = sortValue(a, sort.key);
  const bv = sortValue(b, sort.key);
  if (typeof av === 'string' || typeof bv === 'string') return String(av).localeCompare(String(bv)) * sort.dir;
  if (av == null && bv == null) return 0;
  if (av == null) return 1;
  if (bv == null) return -1;
  return (av - bv) * sort.dir;
}

function heat(v, max) {
  if (v == null || !max || v === 0) return '';
  const a = (0.08 + 0.52 * Math.min(1, Math.abs(v) / max)).toFixed(3);
  return ` style="background-color: rgba(var(--${v > 0 ? 'pos' : 'neg'}-rgb), ${a})"`;
}

function update() {
  flows.rows.forEach((r) => { r.stats = stats(r); });
  renderKpis();

  const start = startIndex();
  const end = flows.months.length;
  const monthIdx = [];
  for (let i = end - 1; i >= start; i--) monthIdx.push(i);

  const head = [
    { key: 'name', label: 'Market', cls: 'l sticky' },
    { key: 'net', label: `Net ${rangeLabel()}`, title: 'Sum of monthly net purchases in the selected period' },
    { key: 'avg', label: 'Avg / month' },
    { key: 'last3', label: 'Last 3M', title: 'Net purchases in the latest 3 months' },
    { key: 'pace', label: '3M vs avg', title: 'Average of the last 3 months minus the average month in the period. Positive = buying picking up or selling easing' },
    { key: 'buying', label: 'Months bought', cls: 'divider', title: 'Months with net buying out of months with data in the period' },
    ...monthIdx.map((i) => ({ key: `m:${i}`, label: monthLabel(flows.months[i]), cls: `month${flows.months[i].endsWith('-01') ? ' jan' : ''}` })),
  ];
  document.getElementById('head').innerHTML = head
    .map((h) => `<th data-key="${h.key}" class="${h.cls || ''}"${h.title ? ` title="${esc(h.title)}"` : ''}${sort.key === h.key ? ` aria-sort="${sort.dir < 0 ? 'descending' : 'ascending'}"` : ''}>${esc(h.label)}</th>`)
    .join('');

  const total = flows.rows.find((r) => r.kind === 'total');
  const small = view === 'country'
    ? flows.rows.filter((r) => r.kind === 'country').sort((a, b) => a.stats.activity - b.stats.activity).slice(0, HIDE_SMALLEST)
    : [];
  const smallNote = document.getElementById('small-note');
  smallNote.classList.toggle('hidden', !small.length);
  smallNote.innerHTML = showSmall
    ? `Showing all countries. <button type="button" class="link-btn">Hide the ${HIDE_SMALLEST} smallest</button>`
    : `Hiding the ${HIDE_SMALLEST} countries with the least money moved ${range > 0 ? `in the last ${rangeLabel()}` : rangeLabel()}: ${small.map((r) => esc(r.name)).join(', ')}. <button type="button" class="link-btn">Show all</button>`;
  const hidden = new Set(showSmall ? [] : small);
  const rows = flows.rows
    .filter((r) => r.kind === view && !hidden.has(r))
    .filter((r) => !query || r.name.toLowerCase().includes(query) || (r.region || '').toLowerCase().includes(query))
    .sort(compare);

  const line = (r, cls) => {
    const s = r.stats;
    const max = Math.max(0, ...monthIdx.map((i) => Math.abs(r.values[i] ?? 0)));
    return `<tr class="${cls}">
      <td class="l sticky name">${esc(r.name)}${r.region ? `<span class="sub">${esc(r.region)}</span>` : ''}</td>
      <td class="num summary ${signClass(s.net)}">${num(s.net)}</td>
      <td class="num ${signClass(s.avg)}">${num(s.avg)}</td>
      <td class="num ${signClass(s.last3)}">${num(s.last3)}</td>
      <td class="num ${signClass(s.pace)}">${s.pace == null ? '·' : `${s.pace > 0 ? '▲' : s.pace < 0 ? '▼' : ''} ${num(s.pace)}`}</td>
      <td class="num muted divider">${s.months ? `${s.buying}/${s.months}` : '·'}</td>
      ${monthIdx.map((i) => `<td class="num month${r.values[i] == null ? ' empty' : ''}"${heat(r.values[i], max)}>${num(r.values[i])}</td>`).join('')}
    </tr>`;
  };

  document.getElementById('body').innerHTML = (total ? line(total, 'total') : '') + rows.map((r) => line(r, '')).join('');
  document.getElementById('empty').classList.toggle('hidden', rows.length > 0);
}

load();
