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

  document.getElementById('supply-top').textContent = fmt(z.supplyTop);
  document.getElementById('supply-btm').textContent = fmt(z.supplyBtm);
  document.getElementById('supply-avg').textContent = fmt(z.supplyAvg);
  document.getElementById('supply-wavg').textContent = fmt(z.supplyWavg);

  document.getElementById('demand-top').textContent = fmt(z.demandTop);
  document.getElementById('demand-btm').textContent = fmt(z.demandBtm);
  document.getElementById('demand-avg').textContent = fmt(z.demandAvg);
  document.getElementById('demand-wavg').textContent = fmt(z.demandWavg);

  document.getElementById('equi-avg').textContent = fmt(z.equilibriumAvg);
  document.getElementById('equi-wavg').textContent = fmt(z.equilibriumWavg);

  const lastClose = data.bars.length ? data.bars[data.bars.length - 1].close : null;
  document.getElementById('last-close').textContent = fmt(lastClose);

  let position = 'Between zones';
  if (lastClose !== null) {
    if (z.supplyReached && lastClose >= z.supplyBtm && lastClose <= z.supplyTop) position = 'In supply zone';
    else if (z.demandReached && lastClose <= z.demandTop && lastClose >= z.demandBtm) position = 'In demand zone';
  }
  document.getElementById('zone-position').textContent = position;

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

// Fits the full loaded range and clears any interval selection.
function resetZoom() {
  if (!barsAscending.length) return;
  chart.timeScale().fitContent();
  activeRange = null;
  document.querySelectorAll('.range-btn').forEach(btn => btn.classList.remove('active'));
  document.getElementById('reset-zoom').classList.add('active');
  scheduleZoneBoxUpdate();
}

function applyRange(rangeKey) {
  if (!barsAscending.length) return;
  const lastBar = barsAscending[barsAscending.length - 1];
  const startBar = findStartBarForDays(RANGE_DAYS[rangeKey]);

  chart.timeScale().setVisibleRange({ from: startBar.time, to: lastBar.time });

  activeRange = rangeKey;
  document.getElementById('reset-zoom').classList.remove('active');
  document.querySelectorAll('.range-btn').forEach(btn => {
    btn.classList.toggle('active', btn.dataset.range === rangeKey);
  });
  scheduleZoneBoxUpdate();
}

document.querySelectorAll('.range-btn').forEach(btn => {
  btn.addEventListener('click', () => applyRange(btn.dataset.range));
});

document.getElementById('reset-zoom').addEventListener('click', resetZoom);

window.addEventListener('keydown', e => {
  if (e.altKey && e.key.toLowerCase() === 'r') {
    e.preventDefault();
    resetZoom();
  }
});

document.getElementById('refresh').addEventListener('click', async () => {
  statusEl.textContent = 'Refreshing from Yahoo Finance…';
  const resp = await fetch('/api/refresh', { method: 'POST' });
  if (!resp.ok) {
    statusEl.textContent = `Refresh failed: ${await resp.text()}`;
    return;
  }
  await loadData();
});

loadData();
