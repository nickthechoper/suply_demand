const chartWrapEl = document.getElementById('chart-wrap');
const chartEl = document.getElementById('chart');
const supplyBoxEl = document.getElementById('supply-box');
const demandBoxEl = document.getElementById('demand-box');
const statusEl = document.getElementById('status');

const chart = LightweightCharts.createChart(chartEl, {
  layout: { background: { color: '#0e1117' }, textColor: '#e6e6e6' },
  grid: {
    vertLines: { color: '#1c2128' },
    horzLines: { color: '#1c2128' },
  },
  timeScale: { timeVisible: false, borderColor: '#2a3038' },
  rightPriceScale: { borderColor: '#2a3038' },
  crosshair: { mode: LightweightCharts.CrosshairMode.Normal },
});

const candleSeries = chart.addCandlestickSeries({
  upColor: '#26a69a',
  downColor: '#ef5350',
  borderVisible: false,
  wickUpColor: '#26a69a',
  wickDownColor: '#ef5350',
});

// Volume histogram squeezed into the bottom of the same pane via its own
// overlay price scale (priceScaleId: '' hides its axis), the standard
// lightweight-charts recipe for a "volume below the chart" panel.
const volumeSeries = chart.addHistogramSeries({
  priceFormat: { type: 'volume' },
  priceScaleId: '',
});
volumeSeries.priceScale().applyOptions({
  scaleMargins: { top: 0.82, bottom: 0 },
});

let equilibriumLine = null;
let currentZones = null;
let barsAscending = [];
let barTimeToIndex = new Map();
let currentSymbol = '';

new ResizeObserver(entries => {
  for (const entry of entries) {
    chart.applyOptions({ width: entry.contentRect.width, height: entry.contentRect.height });
  }
  scheduleZoneBoxUpdate();
}).observe(chartWrapEl);

chart.timeScale().subscribeVisibleLogicalRangeChange(() => updateZoneBoxes());

function positionBox(el, topPrice, bottomPrice, visible) {
  if (!visible) {
    el.style.display = 'none';
    return;
  }
  const yTop = candleSeries.priceToCoordinate(topPrice);
  const yBottom = candleSeries.priceToCoordinate(bottomPrice);
  if (yTop === null || yBottom === null) {
    el.style.display = 'none';
    return;
  }
  el.style.display = 'block';
  el.style.top = `${Math.min(yTop, yBottom)}px`;
  el.style.height = `${Math.max(1, Math.abs(yBottom - yTop))}px`;
}

function updateZoneBoxes() {
  if (!currentZones) return;
  const z = currentZones;
  positionBox(supplyBoxEl, z.supplyTop, z.supplyBtm, z.supplyReached);
  positionBox(demandBoxEl, z.demandTop, z.demandBtm, z.demandReached);
}

// The chart's price scale isn't laid out synchronously after setData/fitContent,
// so priceToCoordinate can briefly return null; retry across a couple of frames
// plus a short timeout to catch it once layout settles.
function scheduleZoneBoxUpdate() {
  requestAnimationFrame(() => {
    updateZoneBoxes();
    requestAnimationFrame(updateZoneBoxes);
  });
  setTimeout(updateZoneBoxes, 100);
}

function fmt(n) {
  if (n === undefined || n === null || Number.isNaN(n)) return '–';
  return n.toFixed(2);
}

function fmtCompact(n) {
  if (n === undefined || n === null || Number.isNaN(n)) return 'n/a';
  const abs = Math.abs(n);
  const sign = n < 0 ? '-' : '';
  if (abs >= 1e12) return sign + (abs / 1e12).toFixed(2) + 'T';
  if (abs >= 1e9) return sign + (abs / 1e9).toFixed(2) + 'B';
  if (abs >= 1e6) return sign + (abs / 1e6).toFixed(2) + 'M';
  if (abs >= 1e3) return sign + (abs / 1e3).toFixed(2) + 'K';
  return sign + abs.toFixed(2);
}

function fmtMoney(n) {
  return n === undefined || n === null || Number.isNaN(n) ? 'n/a' : '$' + fmtCompact(n);
}

// Ownership/insider percentages come from Yahoo as fractions (0.0783), unlike
// the pre-converted percentages already in the fundamentals table.
function fmtFractionPct(n) {
  if (n === undefined || n === null || Number.isNaN(n)) return 'n/a';
  return (n * 100).toFixed(2) + '%';
}

const legendEls = {
  symbol: document.getElementById('legend-symbol'),
  open: document.getElementById('legend-open'),
  high: document.getElementById('legend-high'),
  low: document.getElementById('legend-low'),
  close: document.getElementById('legend-close'),
  change: document.getElementById('legend-change'),
};

function updateLegend(bar, prevBar) {
  if (!bar) return;
  legendEls.symbol.textContent = currentSymbol;
  legendEls.open.textContent = fmt(bar.open);
  legendEls.high.textContent = fmt(bar.high);
  legendEls.low.textContent = fmt(bar.low);
  legendEls.close.textContent = fmt(bar.close);
  if (prevBar) {
    const change = bar.close - prevBar.close;
    const pct = (change / prevBar.close) * 100;
    const sign = change >= 0 ? '+' : '';
    legendEls.change.textContent = `${sign}${change.toFixed(2)} (${sign}${pct.toFixed(2)}%)`;
    legendEls.change.style.color = change >= 0 ? '#26a69a' : '#ef5350';
  } else {
    legendEls.change.textContent = '';
  }
}

function updateFundamentals(f) {
  const peEl = document.getElementById('fund-pe');
  const pegEl = document.getElementById('fund-peg');
  const revEl = document.getElementById('fund-rev-growth');
  const marginEl = document.getElementById('fund-net-margin');

  peEl.textContent = f && f.peRatio != null ? f.peRatio.toFixed(2) : 'n/a';
  pegEl.textContent = f && f.pegRatio != null ? f.pegRatio.toFixed(2) : 'n/a';

  if (f && f.revenueGrowthPct != null) {
    revEl.textContent = `${f.revenueGrowthPct.toFixed(2)}%`;
    revEl.style.color = f.revenueGrowthPct >= 0 ? '#26a69a' : '#ef5350';
  } else {
    revEl.textContent = 'n/a';
    revEl.style.color = '';
  }

  if (f && f.netMarginPct != null) {
    marginEl.textContent = `${f.netMarginPct.toFixed(2)}%`;
    marginEl.style.color = f.netMarginPct >= 0 ? '#26a69a' : '#ef5350';
  } else {
    marginEl.textContent = 'n/a';
    marginEl.style.color = '';
  }
}

chart.subscribeCrosshairMove(param => {
  if (!barsAscending.length) return;
  if (!param || !param.time) {
    const lastIdx = barsAscending.length - 1;
    updateLegend(barsAscending[lastIdx], barsAscending[lastIdx - 1]);
    return;
  }
  const idx = barTimeToIndex.get(param.time);
  if (idx === undefined) return;
  updateLegend(barsAscending[idx], idx > 0 ? barsAscending[idx - 1] : null);
});

async function loadData() {
  statusEl.textContent = 'Loading…';
  const resp = await fetch('/api/data');
  if (!resp.ok) {
    statusEl.textContent = `Error: ${await resp.text()}`;
    return;
  }
  const data = await resp.json();
  document.getElementById('symbol-label').textContent = data.symbol;
  currentSymbol = data.symbol;

  barsAscending = data.bars;
  barTimeToIndex = new Map(barsAscending.map((b, i) => [b.time, i]));
  updateAllRangePerformance();

  candleSeries.setData(barsAscending.map(b => ({
    time: b.time,
    open: b.open,
    high: b.high,
    low: b.low,
    close: b.close,
  })));

  volumeSeries.setData(barsAscending.map(b => ({
    time: b.time,
    value: b.volume,
    color: b.close >= b.open ? 'rgba(38, 166, 154, 0.5)' : 'rgba(239, 83, 80, 0.5)',
  })));

  updateLegend(barsAscending[barsAscending.length - 1], barsAscending[barsAscending.length - 2]);
  updateFundamentals(data.fundamentals);

  const z = data.zones;
  currentZones = z;

  if (equilibriumLine) {
    candleSeries.removePriceLine(equilibriumLine);
    equilibriumLine = null;
  }
  if (z.supplyReached && z.demandReached) {
    equilibriumLine = candleSeries.createPriceLine({
      price: z.equilibriumAvg,
      color: '#9a9a9a',
      lineWidth: 1,
      lineStyle: LightweightCharts.LineStyle.Dashed,
      axisLabelVisible: true,
      title: 'Equilibrium',
    });
  }

  if (activeRange) {
    applyRange(activeRange);
  } else {
    resetZoom();
  }
  scheduleZoneBoxUpdate();
  statusEl.textContent = `Loaded ${data.bars.length} bars. Last updated ${new Date().toLocaleTimeString()}.`;
}

const RANGE_DAYS = { '1D': 1, '1W': 7, '1M': 30, '3M': 91, '6M': 182, '1Y': 365, '2Y': 730 };
let activeRange = '2Y';

// Finds the bar closest to `days` calendar days before the last loaded bar,
// clamped so the window always has at least two bars.
function findStartBarForDays(days) {
  const lastBar = barsAscending[barsAscending.length - 1];
  const targetTime = lastBar.time - days * 86400;
  let startIdx = barsAscending.findIndex(b => b.time >= targetTime);
  if (startIdx === -1) startIdx = 0;
  if (startIdx >= barsAscending.length - 1) startIdx = Math.max(0, barsAscending.length - 2);
  return barsAscending[startIdx];
}

function setPctLabel(key, fromBar, toBar) {
  const el = document.querySelector(`.range-pct[data-pct="${key}"]`);
  if (!el) return;
  const change = toBar.close - fromBar.close;
  const pct = (change / fromBar.close) * 100;
  const sign = change >= 0 ? '+' : '';
  el.textContent = `${sign}${pct.toFixed(2)}%`;
  el.style.color = change >= 0 ? '#26a69a' : '#ef5350';
}

// Computes and displays performance for every range button at once, so the
// figures are visible without clicking through each one.
function updateAllRangePerformance() {
  if (!barsAscending.length) return;
  const lastBar = barsAscending[barsAscending.length - 1];
  for (const key of Object.keys(RANGE_DAYS)) {
    setPctLabel(key, findStartBarForDays(RANGE_DAYS[key]), lastBar);
  }
}

// Re-fits the chart to the currently selected interval (undoing any manual
// pan/zoom) without changing which interval is selected. Falls back to the
// full loaded range if no interval is active.
function resetZoom() {
  if (!barsAscending.length) return;
  if (activeRange) {
    applyRange(activeRange);
  } else {
    chart.timeScale().fitContent();
    scheduleZoneBoxUpdate();
  }
}

function applyRange(rangeKey) {
  if (!barsAscending.length) return;
  const lastBar = barsAscending[barsAscending.length - 1];
  const startBar = findStartBarForDays(RANGE_DAYS[rangeKey]);

  chart.timeScale().setVisibleRange({ from: startBar.time, to: lastBar.time });

  activeRange = rangeKey;
  document.querySelectorAll('.range-btn').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.range === rangeKey);
  });
  scheduleZoneBoxUpdate();
}

document.querySelectorAll('.range-btn[data-range]').forEach(btn => {
  btn.addEventListener('click', () => applyRange(btn.dataset.range));
});

document.getElementById('reset-zoom').addEventListener('click', resetZoom);

window.addEventListener('keydown', e => {
  if (e.altKey && e.key.toLowerCase() === 'r') {
    e.preventDefault();
    resetZoom();
  }
});

function renderAnalyst(analyst) {
  const el = document.getElementById('analyst-body');
  if (!analyst || analyst.targetMean == null) {
    el.innerHTML = '<span class="empty-note">No analyst data available.</span>';
    return;
  }

  const currentPrice = barsAscending.length ? barsAscending[barsAscending.length - 1].close : null;
  const ratingLabel = analyst.recommendationKey
    ? analyst.recommendationKey.replace(/_/g, ' ').toUpperCase()
    : 'n/a';

  let rangeHtml = '';
  if (analyst.targetLow != null && analyst.targetHigh != null && analyst.targetHigh > analyst.targetLow) {
    const pos = v => Math.max(0, Math.min(100, ((v - analyst.targetLow) / (analyst.targetHigh - analyst.targetLow)) * 100));
    const markers = [{ pos: pos(analyst.targetMean), cls: 'mean', label: `Mean ${fmt(analyst.targetMean)}` }];
    if (currentPrice != null) markers.push({ pos: pos(currentPrice), cls: 'current', label: `Now ${fmt(currentPrice)}` });
    rangeHtml = `
      <div class="target-range">
        <div class="track"></div>
        ${markers.map(m => `<div class="marker ${m.cls}" style="left:${m.pos}%"><span class="marker-label">${m.label}</span></div>`).join('')}
      </div>
      <div class="stat-row"><span>Low</span><b>${fmt(analyst.targetLow)}</b></div>
      <div class="stat-row"><span>High</span><b>${fmt(analyst.targetHigh)}</b></div>
    `;
  }

  el.innerHTML = `
    <div class="stat-row"><span>Rating</span><b>${ratingLabel}${analyst.recommendationMean != null ? ` (${fmt(analyst.recommendationMean)})` : ''}</b></div>
    <div class="stat-row"><span>Analysts</span><b>${analyst.numAnalysts != null ? Math.round(analyst.numAnalysts) : 'n/a'}</b></div>
    <div class="stat-row"><span>Target Mean</span><b>${fmt(analyst.targetMean)}</b></div>
    <div class="stat-row"><span>Target Median</span><b>${fmt(analyst.targetMedian)}</b></div>
    ${rangeHtml}
  `;
}

function renderPeers(peers, symbol) {
  const el = document.getElementById('peers-body');
  if (!peers || !peers.length) {
    el.innerHTML = '<span class="empty-note">No peer data available.</span>';
    return;
  }
  const rows = peers.map(p => `
    <tr class="${p.symbol === symbol ? 'highlight-row' : ''}">
      <td>${p.symbol}${p.symbol === symbol ? ' (this)' : ''}</td>
      <td>${p.trailingPE != null ? fmt(p.trailingPE) : 'n/a'}</td>
      <td>${p.forwardPE != null ? fmt(p.forwardPE) : 'n/a'}</td>
      <td>${fmtMoney(p.marketCap)}</td>
    </tr>
  `).join('');
  el.innerHTML = `<table><tr><th>Symbol</th><th>P/E</th><th>Fwd P/E</th><th>Mkt Cap</th></tr>${rows}</table>`;
}

function renderProfile(profile) {
  const el = document.getElementById('profile-body');
  if (!profile || !profile.sector) {
    el.innerHTML = '<span class="empty-note">No company profile available.</span>';
    return;
  }
  el.innerHTML = `
    <div class="stat-row"><span>Sector</span><b>${profile.sector || 'n/a'}</b></div>
    <div class="stat-row"><span>Industry</span><b>${profile.industry || 'n/a'}</b></div>
    <div class="stat-row"><span>Employees</span><b>${profile.employees != null ? Math.round(profile.employees).toLocaleString() : 'n/a'}</b></div>
    <div class="stat-row"><span>Website</span><b><a href="${profile.website}" target="_blank" rel="noopener">${profile.website || 'n/a'}</a></b></div>
    <div class="stat-row"><span>Address</span><b>${profile.address || 'n/a'}</b></div>
    <div class="summary-text" style="margin-top:10px;">${profile.summary || ''}</div>
  `;
}

function holderRows(holders) {
  if (!holders || !holders.length) return '<tr><td colspan="3" class="empty-note">n/a</td></tr>';
  return holders.slice(0, 8).map(h => `
    <tr>
      <td>${h.organization}</td>
      <td>${fmtFractionPct(h.pctHeld)}</td>
      <td>${fmtMoney(h.value)}</td>
    </tr>
  `).join('');
}

function renderOwnership(ownership) {
  const el = document.getElementById('ownership-body');
  if (!ownership) {
    el.innerHTML = '<span class="empty-note">No ownership data available.</span>';
    return;
  }
  const insiderRows = (ownership.insiderTransactions || []).slice(0, 8).map(t => `
    <tr>
      <td>${t.filerName || 'n/a'}</td>
      <td>${t.relation || 'n/a'}</td>
      <td>${t.shares != null ? Math.round(t.shares).toLocaleString() : 'n/a'}</td>
      <td>${t.date || 'n/a'}</td>
    </tr>
  `).join('') || '<tr><td colspan="4" class="empty-note">n/a</td></tr>';

  const net = ownership.netInsiderActivity || {};

  el.innerHTML = `
    <div style="display:grid; grid-template-columns: repeat(auto-fit, minmax(260px,1fr)); gap:16px;">
      <div>
        <div class="stat-row"><span>Insiders Hold</span><b>${fmtFractionPct(ownership.insidersPct)}</b></div>
        <div class="stat-row"><span>Institutions Hold</span><b>${fmtFractionPct(ownership.institutionsPct)}</b></div>
        <div class="stat-row"><span>Institution Count</span><b>${ownership.institutionsCount != null ? Math.round(ownership.institutionsCount).toLocaleString() : 'n/a'}</b></div>
        <div class="stat-row"><span>Net Insider Shares (${net.period || 'n/a'})</span><b>${net.netShares != null ? Math.round(net.netShares).toLocaleString() : 'n/a'}</b></div>
      </div>
      <div>
        <h4 style="margin:0 0 6px; font-size:12px; color:var(--muted);">Top Institutional Holders</h4>
        <div class="scroll-list"><table><tr><th>Holder</th><th>% Held</th><th>Value</th></tr>${holderRows(ownership.topInstitutions)}</table></div>
      </div>
      <div>
        <h4 style="margin:0 0 6px; font-size:12px; color:var(--muted);">Top Fund Holders</h4>
        <div class="scroll-list"><table><tr><th>Fund</th><th>% Held</th><th>Value</th></tr>${holderRows(ownership.topFunds)}</table></div>
      </div>
      <div>
        <h4 style="margin:0 0 6px; font-size:12px; color:var(--muted);">Recent Insider Transactions</h4>
        <div class="scroll-list"><table><tr><th>Filer</th><th>Relation</th><th>Shares</th><th>Date</th></tr>${insiderRows}</table></div>
      </div>
    </div>
  `;
}

function renderFinancials(financials, balanceSheet, cashFlow) {
  const el = document.getElementById('financials-body');
  const rows = (financials || []).map(f => `
    <tr>
      <td>${f.endDate}</td>
      <td>${fmtMoney(f.totalRevenue)}</td>
      <td>${fmtMoney(f.netIncome)}</td>
    </tr>
  `).join('') || '<tr><td colspan="3" class="empty-note">n/a</td></tr>';

  el.innerHTML = `
    <table><tr><th>Year End</th><th>Revenue</th><th>Net Income</th></tr>${rows}</table>
    <h4 style="margin:14px 0 6px; font-size:12px; color:var(--muted);">Latest Balance Sheet &amp; Cash Flow</h4>
    <div class="stat-row"><span>Total Cash</span><b>${fmtMoney(balanceSheet && balanceSheet.totalCash)}</b></div>
    <div class="stat-row"><span>Total Debt</span><b>${fmtMoney(balanceSheet && balanceSheet.totalDebt)}</b></div>
    <div class="stat-row"><span>Current Ratio</span><b>${balanceSheet && balanceSheet.currentRatio != null ? fmt(balanceSheet.currentRatio) : 'n/a'}</b></div>
    <div class="stat-row"><span>Quick Ratio</span><b>${balanceSheet && balanceSheet.quickRatio != null ? fmt(balanceSheet.quickRatio) : 'n/a'}</b></div>
    <div class="stat-row"><span>Operating Cash Flow</span><b>${fmtMoney(cashFlow && cashFlow.operatingCashflow)}</b></div>
    <div class="stat-row"><span>Free Cash Flow</span><b>${fmtMoney(cashFlow && cashFlow.freeCashflow)}</b></div>
    <div class="empty-note" style="margin-top:8px; display:block;">Balance sheet &amp; cash flow are latest-available snapshots — Yahoo's free API no longer provides historical detail for these.</div>
  `;
}

function renderFilings(filings) {
  const el = document.getElementById('filings-body');
  if (!filings || !filings.length) {
    el.innerHTML = '<span class="empty-note">No filings available.</span>';
    return;
  }
  el.innerHTML = '<div class="scroll-list">' + filings.slice(0, 12).map(f => `
    <div class="filing-row">
      <span class="filing-type">${f.type || ''}</span>
      <span class="filing-date">${f.date || ''}</span>
      <a class="filing-title" href="${f.url}" target="_blank" rel="noopener">${f.title || f.url}</a>
    </div>
  `).join('') + '</div>';
}

async function loadCompanyData() {
  try {
    const resp = await fetch('/api/company');
    if (!resp.ok) {
      const msg = await resp.text();
      document.querySelectorAll('#company-section .empty-note').forEach(el => {
        el.textContent = `Unavailable: ${msg}`;
      });
      return;
    }
    const data = await resp.json();
    renderAnalyst(data.analyst);
    renderPeers(data.peers, currentSymbol);
    renderProfile(data.profile);
    renderOwnership(data.ownership);
    renderFinancials(data.financials, data.balanceSheet, data.cashFlow);
    renderFilings(data.filings);
  } catch (e) {
    console.error('loading company data failed', e);
  }
}

document.getElementById('refresh').addEventListener('click', async () => {
  statusEl.textContent = 'Refreshing from Yahoo Finance…';
  const resp = await fetch('/api/refresh', { method: 'POST' });
  if (!resp.ok) {
    statusEl.textContent = `Refresh failed: ${await resp.text()}`;
    return;
  }
  await loadData();
  await loadCompanyData();
});

loadData().then(loadCompanyData);
