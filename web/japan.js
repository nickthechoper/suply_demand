const resultEl = document.getElementById('result');
const periodEl = document.getElementById('period');

const css = getComputedStyle(document.documentElement);
const token = (name) => css.getPropertyValue(name).trim();
const SERIES_COLORS = [1, 2, 3, 4, 5, 6].map((i) => token(`--series-${i}`));
const TEXT = token('--text');
const MUTED = token('--muted');
const BORDER = token('--border');
const PANEL = token('--panel');

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const RANGES = [
  { label: '3Y', months: 36 },
  { label: '5Y', months: 60 },
  { label: 'All', months: 0 },
];
const AXIS_WIDTH = 52;
const TOP_LINES = 6;
const POS = token('--pos');
const NEG = token('--neg');

let flows = null;
let charts = [];
let range = 0;
let view = 'chart';
let cumRange = 0;
let cumCharts = [];

function monthLabel(ym, long) {
  const [y, m] = ym.split('-');
  return long ? `${MONTHS[+m - 1]} ${y}` : `${MONTHS[+m - 1]}-${y.slice(2)}`;
}

function fmt(v) {
  if (v == null) return '—';
  const s = Math.abs(v).toLocaleString('en-US', { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  return v > 0 ? `+${s}` : v < 0 ? `−${s}` : s;
}

function yen(v) {
  if (v == null) return '—';
  const s = Math.abs(v / 10).toLocaleString('en-US', { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  return `${v < 0 ? '−' : '+'}¥${s}tn`;
}

function signClass(v) {
  return v > 0 ? 'pos' : v < 0 ? 'neg' : '';
}

function lastIndex(values) {
  for (let i = values.length - 1; i >= 0; i--) if (values[i] != null) return i;
  return -1;
}

function esc(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

async function load() {
  try {
    const res = await fetch('/api/japan-flows');
    if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
    flows = await res.json();
    render();
  } catch (err) {
    resultEl.innerHTML = `
      <div class="note err">
        <h2>Couldn't load the data</h2>
        <p>${esc(err.message)}</p>
      </div>`;
  }
}

function kpi(label, series) {
  const i = lastIndex(series.values);
  const v = series.values[i];
  const prev = i >= 12 ? series.values[i - 12] : null;
  const note = prev == null ? '' : ` · ${fmt(prev)} a year earlier`;
  return `
    <div class="kpi">
      <div class="k">${esc(label)}</div>
      <div class="v num ${signClass(v)}">${fmt(v)}</div>
      <div class="n num">${yen(v)} net, 12 months to ${monthLabel(flows.months[i], true)}${note}</div>
    </div>`;
}

function render() {
  const n = flows.months.length;
  periodEl.textContent = `Japanese net purchases of long-term foreign bonds · rolling ${flows.window}-month sum · ${monthLabel(flows.months[0], true)} – ${monthLabel(flows.months[n - 1], true)}`;

  const combined = {
    values: flows.us.values.map((v, i) => (v == null || flows.totalEA.values[i] == null ? null : v + flows.totalEA.values[i])),
  };

  const legend = flows.euroArea
    .map((s, i) => `<span><i style="background:${SERIES_COLORS[i]}"></i>${esc(s.name)}</span>`)
    .join('');

  resultEl.innerHTML = `
    <div class="kpis">
      ${kpi('Euro area total', flows.totalEA)}
      ${kpi('United States', flows.us)}
      ${kpi('Euro area + US', combined)}
    </div>
    <div class="card">
      <div class="card-head">
        <h3>Net purchases of long-term debt securities</h3>
        <span class="hint">12-month sum, ¥100bn · positive = Japanese buying, negative = selling / repatriation</span>
        <div class="tools">
          <div class="segmented" id="range" role="group" aria-label="Time range">
            ${RANGES.map((r) => `<button type="button" data-months="${r.months}" aria-pressed="${r.months === range}">${r.label}</button>`).join('')}
          </div>
          <div class="segmented" id="view" role="group" aria-label="View">
            <button type="button" data-view="chart" aria-pressed="${view === 'chart'}">Chart</button>
            <button type="button" data-view="table" aria-pressed="${view === 'table'}">Table</button>
          </div>
        </div>
      </div>
      <div class="legend" id="legend">
        ${legend}
        <span><i class="line" style="background:${TEXT}"></i>Total EA</span>
      </div>
      <div class="chart-body" id="chart-wrap">
        <div class="panel-title">Euro area <span>by country of issuer</span></div>
        <div class="panel ea"><canvas id="chart-ea" aria-label="Japanese net purchases of euro-area long-term debt securities by country, 12-month sum"></canvas></div>
        <div class="panel-title">United States</div>
        <div class="panel us"><canvas id="chart-us" aria-label="Japanese net purchases of US long-term debt securities, 12-month sum"></canvas></div>
      </div>
      <div class="table-wrap hidden" id="table-wrap"></div>
    </div>
    <div class="card">
      <div class="card-head">
        <h3>Where the money accumulated</h3>
        <span class="hint" id="cum-hint"></span>
        <div class="tools">
          <div class="segmented" id="cum-range" role="group" aria-label="Accumulation period">
            ${RANGES.map((r) => `<button type="button" data-months="${r.months}" aria-pressed="${r.months === cumRange}">${r.label}</button>`).join('')}
          </div>
        </div>
      </div>
      <div class="cum-body">
        <div>
          <div class="panel-title">Ranking <span>net purchases added up, by country of issuer</span></div>
          <div class="panel cum"><canvas id="chart-rank" aria-label="Countries ranked by Japanese cumulative net purchases of long-term debt securities"></canvas></div>
        </div>
        <div>
          <div class="panel-title">Running total <span>${TOP_LINES} largest</span></div>
          <div class="legend inline" id="cum-legend"></div>
          <div class="panel cum"><canvas id="chart-cum" aria-label="Running total of Japanese net purchases of long-term debt securities by country"></canvas></div>
        </div>
      </div>
    </div>
    <p class="source">
      Other EA = ${esc(flows.otherEA.map((s) => s.name).join(', '))}. Source: Bank of Japan / Ministry of Finance balance of payments (BPM6),
      portfolio investment assets, long-term debt securities, net, by country of issuer. Monthly data starts January 2014, so the 12-month sum starts ${monthLabel(flows.months[0], true)}.
      Fetched ${new Date(flows.fetchedAt * 1000).toLocaleString()}.
    </p>`;

  document.getElementById('range').addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    range = +b.dataset.months;
    document.querySelectorAll('#range button').forEach((x) => x.setAttribute('aria-pressed', String(x === b)));
    applyRange();
    renderTable();
  });
  document.getElementById('view').addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    view = b.dataset.view;
    document.querySelectorAll('#view button').forEach((x) => x.setAttribute('aria-pressed', String(x === b)));
    document.getElementById('chart-wrap').classList.toggle('hidden', view !== 'chart');
    document.getElementById('legend').classList.toggle('hidden', view !== 'chart');
    document.getElementById('table-wrap').classList.toggle('hidden', view !== 'table');
  });

  document.getElementById('cum-range').addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    cumRange = +b.dataset.months;
    document.querySelectorAll('#cum-range button').forEach((x) => x.setAttribute('aria-pressed', String(x === b)));
    drawCumulative();
  });

  drawCharts();
  renderTable();
  drawCumulative();
}

function cumStart() {
  return cumRange > 0 ? Math.max(0, flows.months.length - cumRange) : 0;
}

function cumSince(series) {
  const start = cumStart();
  const base = start > 0 ? series.values[start - 1] : 0;
  return series.values.map((v, i) => (i < start || v == null || base == null ? null : Math.round((v - base) * 100) / 100));
}

const barValueLabels = {
  id: 'barValueLabels',
  afterDatasetsDraw(c) {
    const { ctx } = c;
    const meta = c.getDatasetMeta(0);
    ctx.save();
    ctx.font = '12px -apple-system, Segoe UI, Roboto, Arial, sans-serif';
    ctx.textBaseline = 'middle';
    meta.data.forEach((bar, i) => {
      const v = c.data.datasets[0].data[i];
      if (v == null) return;
      ctx.fillStyle = MUTED;
      ctx.textAlign = v < 0 ? 'right' : 'left';
      ctx.fillText(yen(v), bar.x + (v < 0 ? -6 : 6), bar.y);
    });
    ctx.restore();
  },
};

function drawCumulative() {
  cumCharts.forEach((c) => c.destroy());
  const labels = flows.months;
  const start = cumStart();
  const end = labels.length - 1;
  const since = start > 0 ? monthLabel(labels[start], true) : monthLabel(flows.start, true);
  document.getElementById('cum-hint').textContent = `¥100bn, added up since ${since} to ${monthLabel(labels[end], true)} · positive = money in, negative = money taken out`;

  const ranked = flows.cumulative
    .map((s) => {
      const values = cumSince(s);
      return { name: s.name, values, total: values[lastIndex(values)] ?? null };
    })
    .sort((a, b) => (b.total ?? -Infinity) - (a.total ?? -Infinity));

  const tooltip = baseOptions(true).plugins.tooltip;
  const rank = new Chart(document.getElementById('chart-rank'), {
    type: 'bar',
    data: {
      labels: ranked.map((r) => r.name),
      datasets: [{
        label: 'Accumulated',
        data: ranked.map((r) => r.total),
        backgroundColor: ranked.map((r) => (r.total < 0 ? NEG : POS)),
        borderRadius: 3,
        barPercentage: 0.8,
        categoryPercentage: 0.9,
      }],
    },
    options: {
      indexAxis: 'y',
      responsive: true,
      maintainAspectRatio: false,
      animation: false,
      layout: { padding: { left: 56, right: 64 } },
      plugins: {
        legend: { display: false },
        tooltip: {
          ...tooltip,
          callbacks: {
            title: (items) => items[0].label,
            label: (item) => ` ${fmt(item.raw)}  (${yen(item.raw)}) since ${since}`,
            labelColor: (item) => {
              const color = item.raw < 0 ? NEG : POS;
              return { borderColor: color, backgroundColor: color, borderWidth: 0, borderRadius: 2 };
            },
          },
        },
      },
      scales: {
        x: {
          grid: {
            color: (ctx) => (ctx.tick && ctx.tick.value === 0 ? MUTED : 'rgba(255, 255, 255, 0.05)'),
            lineWidth: (ctx) => (ctx.tick && ctx.tick.value === 0 ? 1.5 : 1),
            drawTicks: false,
          },
          border: { display: false },
          ticks: { color: MUTED, padding: 6, maxTicksLimit: 6, callback: (v) => v.toLocaleString('en-US') },
        },
        y: {
          grid: { display: false },
          border: { display: false },
          ticks: { color: TEXT, autoSkip: false },
        },
      },
    },
    plugins: [barValueLabels],
  });

  const top = [...ranked]
    .filter((r) => r.total != null)
    .sort((a, b) => Math.abs(b.total) - Math.abs(a.total))
    .slice(0, TOP_LINES);
  document.getElementById('cum-legend').innerHTML = top
    .map((r, i) => `<span><i class="line" style="background:${SERIES_COLORS[i]}"></i>${esc(r.name)}</span>`)
    .join('');

  const lineOptions = baseOptions(true);
  lineOptions.scales.x.min = start;
  lineOptions.scales.x.max = end;
  lineOptions.plugins.tooltip.itemSort = (a, b) => b.raw - a.raw;
  lineOptions.plugins.tooltip.callbacks.title = (items) => `${since} to ${monthLabel(labels[items[0].dataIndex], true)}`;
  const lines = new Chart(document.getElementById('chart-cum'), {
    data: { labels, datasets: top.map((r, i) => lineDataset(r.name, r.values, SERIES_COLORS[i])) },
    options: lineOptions,
  });

  cumCharts = [rank, lines];
}

function visibleStart() {
  return range > 0 ? Math.max(0, flows.months.length - range) : 0;
}

function applyRange() {
  for (const c of charts) {
    c.options.scales.x.min = visibleStart();
    c.options.scales.x.max = flows.months.length - 1;
    c.update('none');
  }
}

const syncHover = {
  id: 'syncHover',
  afterEvent(c, args) {
    const e = args.event;
    const others = charts.filter((o) => o !== c);
    if (e.type === 'mouseout') {
      for (const o of others) {
        o.setActiveElements([]);
        o.tooltip.setActiveElements([], { x: 0, y: 0 });
        o.update('none');
      }
      return;
    }
    if (e.type !== 'mousemove') return;
    const hit = c.getElementsAtEventForMode(e, 'index', { intersect: false }, false);
    if (!hit.length) return;
    const index = hit[0].index;
    for (const o of others) {
      const active = o.data.datasets
        .map((d, datasetIndex) => ({ datasetIndex, index }))
        .filter((a) => o.data.datasets[a.datasetIndex].data[index] != null);
      o.setActiveElements(active);
      o.tooltip.setActiveElements(active, { x: o.scales.x.getPixelForValue(index), y: o.chartArea.top + 10 });
      o.update('none');
    }
  },
};

function baseOptions(showX) {
  const labels = flows.months;
  return {
    responsive: true,
    maintainAspectRatio: false,
    animation: false,
    interaction: { mode: 'index', intersect: false },
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: 'rgba(14, 17, 23, 0.96)',
        borderColor: BORDER,
        borderWidth: 1,
        titleColor: TEXT,
        bodyColor: TEXT,
        padding: 10,
        boxWidth: 10,
        boxHeight: 10,
        boxPadding: 4,
        titleFont: { weight: '600' },
        callbacks: {
          title: (items) => `12 months to ${monthLabel(labels[items[0].dataIndex], true)}`,
          label: (item) => ` ${item.dataset.label}: ${fmt(item.raw)}  (${yen(item.raw)})`,
          afterLabel: (item) => (item.dataset.type === 'bar' && flows.euroArea[item.datasetIndex]?.key === 'OTHER' ? flows.otherEA : [])
            .filter((s) => s.values[item.dataIndex] != null)
            .sort((a, b) => Math.abs(b.values[item.dataIndex]) - Math.abs(a.values[item.dataIndex]))
            .map((s) => `      ${s.name}: ${fmt(s.values[item.dataIndex])}`),
          labelColor: (item) => {
            const color = item.dataset.type === 'bar' ? item.dataset.backgroundColor : item.dataset.borderColor;
            return { borderColor: color, backgroundColor: color, borderWidth: 0, borderRadius: 2 };
          },
        },
      },
    },
    scales: {
      x: {
        type: 'category',
        min: visibleStart(),
        grid: { display: false },
        border: { color: BORDER, display: showX },
        ticks: {
          display: showX,
          color: MUTED,
          autoSkip: false,
          maxRotation: 0,
          callback: function (value) {
            const ym = labels[value];
            const span = (this.max ?? labels.length - 1) - (this.min ?? 0);
            const y = +ym.slice(0, 4);
            const m = +ym.slice(5);
            const show = span > 96 ? m === 1 && y % 2 === 0 : span > 48 ? m === 1 : m === 1 || m === 7;
            return show ? monthLabel(ym) : null;
          },
        },
      },
      y: {
        type: 'linear',
        grace: '5%',
        afterFit: (scale) => { scale.width = AXIS_WIDTH; },
        grid: {
          color: (ctx) => (ctx.tick && ctx.tick.value === 0 ? MUTED : 'rgba(255, 255, 255, 0.05)'),
          lineWidth: (ctx) => (ctx.tick && ctx.tick.value === 0 ? 1.5 : 1),
          drawTicks: false,
        },
        border: { display: false },
        ticks: { color: MUTED, padding: 8, maxTicksLimit: 6, callback: (v) => v.toLocaleString('en-US') },
      },
    },
  };
}

function lineDataset(label, data, color, fill) {
  return {
    type: 'line',
    label,
    data,
    order: 1,
    borderColor: color,
    backgroundColor: fill || 'transparent',
    fill: fill ? 'origin' : false,
    borderWidth: 2,
    pointRadius: 0,
    pointHoverRadius: 4,
    pointHoverBackgroundColor: color,
    pointHoverBorderColor: PANEL,
    pointHoverBorderWidth: 2,
    tension: 0,
  };
}

function drawCharts() {
  const labels = flows.months;
  const bars = flows.euroArea.map((s, i) => ({
    type: 'bar',
    label: s.name,
    data: s.values,
    stack: 'ea',
    order: 2,
    backgroundColor: SERIES_COLORS[i],
    borderColor: PANEL,
    borderWidth: { top: 0, bottom: 0, left: 1, right: 1 },
    borderSkipped: false,
    categoryPercentage: 1,
    barPercentage: 1,
  }));
  const totalEA = { ...lineDataset('Total EA', flows.totalEA.values, TEXT), stack: 'total' };

  const eaOptions = baseOptions(false);
  eaOptions.scales.y.stacked = true;
  eaOptions.plugins.tooltip.itemSort = (a, b) => (a.dataset.type === 'line') - (b.dataset.type === 'line') || a.datasetIndex - b.datasetIndex;

  charts = [
    new Chart(document.getElementById('chart-ea'), {
      data: { labels, datasets: [...bars, totalEA] },
      options: eaOptions,
      plugins: [syncHover],
    }),
    new Chart(document.getElementById('chart-us'), {
      data: { labels, datasets: [lineDataset('United States', flows.us.values, SERIES_COLORS[0], 'rgba(57, 135, 229, 0.12)')] },
      options: baseOptions(true),
      plugins: [syncHover],
    }),
  ];
}

function renderTable() {
  const start = visibleStart();
  const cols = [...flows.euroArea, ...flows.otherEA, flows.totalEA, flows.us];
  const rows = [];
  for (let i = flows.months.length - 1; i >= start; i--) {
    rows.push(`<tr><td class="l">${monthLabel(flows.months[i], true)}</td>${cols
      .map((s) => `<td class="num ${s === flows.totalEA || s === flows.us ? 'strong ' + signClass(s.values[i]) : flows.otherEA.includes(s) ? 'sub' : ''}">${fmt(s.values[i])}</td>`)
      .join('')}</tr>`);
  }
  document.getElementById('table-wrap').innerHTML = `
    <table>
      <thead><tr><th class="l">12 months to</th>${cols.map((s) => `<th${flows.otherEA.includes(s) ? ' class="sub" title="Part of Other EA"' : ''}>${esc(s.name)}</th>`).join('')}</tr></thead>
      <tbody>${rows.join('')}</tbody>
    </table>`;
}

load();
