const PRICE_SCALE_WIDTH = 80;

const chartWrapEl = document.getElementById('chart-wrap');
const chartEl = document.getElementById('chart');
const supplyBoxEl = document.getElementById('supply-box');
const demandBoxEl = document.getElementById('demand-box');
const statusEl = document.getElementById('status');

const WEEKDAY_NAMES = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const MONTH_NAMES = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

function tickMarkFormatter(time, tickMarkType) {
  const date = new Date(time * 1000);
  const pad = n => String(n).padStart(2, '0');
  switch (tickMarkType) {
    case LightweightCharts.TickMarkType.Time:
      return `${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}`;
    case LightweightCharts.TickMarkType.TimeWithSeconds:
      return `${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}:${pad(date.getUTCSeconds())}`;
    default:
      return `${WEEKDAY_NAMES[date.getUTCDay()]} ${date.getUTCDate()} ${MONTH_NAMES[date.getUTCMonth()]}`;
  }
}

const chart = LightweightCharts.createChart(chartEl, {
  layout: { background: { color: '#0e1117' }, textColor: '#e6e6e6' },
  grid: {
    vertLines: { color: '#1c2128' },
    horzLines: { color: '#1c2128' },
  },
  timeScale: { timeVisible: false, borderColor: '#2a3038', tickMarkFormatter },
  rightPriceScale: {
    borderColor: '#2a3038',
    scaleMargins: { top: 0.1, bottom: 0.2 },
    minimumWidth: PRICE_SCALE_WIDTH,
  },
  crosshair: { mode: LightweightCharts.CrosshairMode.Normal },
  handleScroll: { mouseWheel: false },
  handleScale: { mouseWheel: false, pinch: false, axisPressedMouseMove: false, axisDoubleClickReset: false },
});

const candleSeries = chart.addCandlestickSeries({
  upColor: '#26a69a',
  downColor: '#ef5350',
  borderVisible: false,
  wickUpColor: '#26a69a',
  wickDownColor: '#ef5350',
});

const volumeSeries = chart.addHistogramSeries({
  priceFormat: { type: 'volume' },
  priceScaleId: '',
});
volumeSeries.priceScale().applyOptions({
  scaleMargins: { top: 0.82, bottom: 0 },
});

function createIndicatorChart(el) {
  return LightweightCharts.createChart(el, {
    layout: { background: { color: '#0e1117' }, textColor: '#e6e6e6' },
    grid: {
      vertLines: { color: '#1c2128' },
      horzLines: { color: '#1c2128' },
    },
    timeScale: { visible: false, borderColor: '#2a3038' },
    rightPriceScale: { borderColor: '#2a3038', minimumWidth: PRICE_SCALE_WIDTH },
    crosshair: { mode: LightweightCharts.CrosshairMode.Magnet },
    handleScroll: false,
    handleScale: false,
  });
}

const bondChartEl = document.getElementById('bond-pane-chart');
const atrChartEl = document.getElementById('atr-chart');
const rsiChartEl = document.getElementById('rsi-chart');
const rsiDiffChartEl = document.getElementById('rsi-diff-chart');

const bondChart = createIndicatorChart(bondChartEl);
const atrChart = createIndicatorChart(atrChartEl);
const rsiChart = createIndicatorChart(rsiChartEl);
const rsiDiffChart = createIndicatorChart(rsiDiffChartEl);

const bondSeries = bondChart.addLineSeries({
  color: '#ffb74d',
  lineWidth: 2,
  priceFormat: { type: 'custom', formatter: v => `${v.toFixed(2)}%` },
});

const atrSeries = atrChart.addLineSeries({ color: '#ef5350', lineWidth: 2 });

const rsiSeries = rsiChart.addLineSeries({ color: '#b39ddb', lineWidth: 2 });
const rsiMaSeries = rsiChart.addLineSeries({ color: '#ffd54f', lineWidth: 1, priceLineVisible: false });
[70, 50, 30].forEach(level => {
  rsiSeries.createPriceLine({
    price: level,
    color: level === 50 ? 'rgba(120,123,134,0.5)' : '#787b86',
    lineWidth: 1,
    lineStyle: LightweightCharts.LineStyle.Dashed,
    axisLabelVisible: false,
  });
});

const rsiDiffSeries = rsiDiffChart.addBaselineSeries({
  baseValue: { type: 'price', price: 0 },
  topLineColor: 'rgba(38, 166, 154, 1)',
  topFillColor1: 'rgba(38, 166, 154, 0.28)',
  topFillColor2: 'rgba(38, 166, 154, 0.05)',
  bottomLineColor: 'rgba(239, 83, 80, 1)',
  bottomFillColor1: 'rgba(239, 83, 80, 0.05)',
  bottomFillColor2: 'rgba(239, 83, 80, 0.28)',
  lineWidth: 2,
});

const allCharts = [chart, bondChart, atrChart, rsiChart, rsiDiffChart];
const allChartEls = [chartEl, bondChartEl, atrChartEl, rsiChartEl, rsiDiffChartEl];

const WHEEL_ZOOM_STEP = 0.1;

function onWheelZoom(chartInstance, containerEl) {
  return e => {
    if (!e.ctrlKey) return;
    e.preventDefault();
    const range = chartInstance.timeScale().getVisibleLogicalRange();
    if (!range) return;
    const rect = containerEl.getBoundingClientRect();
    const pivot = chartInstance.timeScale().coordinateToLogical(e.clientX - rect.left);
    if (pivot === null) return;
    const factor = e.deltaY < 0 ? 1 - WHEEL_ZOOM_STEP : 1 + WHEEL_ZOOM_STEP;
    chartInstance.timeScale().setVisibleLogicalRange({
      from: pivot - (pivot - range.from) * factor,
      to: pivot + (range.to - pivot) * factor,
    });
  };
}

allCharts.forEach((c, i) => {
  allChartEls[i].addEventListener('wheel', onWheelZoom(c === bondChart ? chart : c, allChartEls[i]), { passive: false });
});

const bondWrapEl = document.getElementById('bond-wrap');

let syncingRange = false;
allCharts.forEach(c => {
  if (c === bondChart) return;
  c.timeScale().subscribeVisibleLogicalRangeChange(range => {
    if (syncingRange || !range) return;
    syncingRange = true;
    allCharts.forEach(other => {
      if (other !== c) other.timeScale().setVisibleLogicalRange(range);
    });
    syncingRange = false;
  });
});

function alignBondChart() {
  const range = chart.timeScale().getVisibleLogicalRange();
  if (range) bondChart.timeScale().setVisibleLogicalRange(range);
}

let syncingCrosshair = false;
const crosshairSeriesByChart = new Map([
  [chart, candleSeries],
  [bondChart, bondSeries],
  [atrChart, atrSeries],
  [rsiChart, rsiSeries],
  [rsiDiffChart, rsiDiffSeries],
]);
function crosshairValueForChart(otherChart, idx) {
  if (idx === undefined) return 0;
  if (otherChart === chart) return barsAscending[idx].close;
  if (otherChart === bondChart) return bondValues[idx] ?? 0;
  if (otherChart === atrChart) return atrValues[idx] ?? 0;
  if (otherChart === rsiChart) return rsiValues[idx] ?? 0;
  return rsiDiffValues[idx] ?? 0;
}
allCharts.forEach(c => {
  c.subscribeCrosshairMove(param => {
    if (syncingCrosshair) return;
    syncingCrosshair = true;
    if (param && param.time) {
      const idx = barTimeToIndex.get(param.time);
      if (c === rsiChart && idx !== undefined) {
        rsiChart.setCrosshairPosition(crosshairValueForChart(rsiChart, idx), param.time, rsiSeries);
      }
      allCharts.forEach(other => {
        if (other === c) return;
        other.setCrosshairPosition(crosshairValueForChart(other, idx), param.time, crosshairSeriesByChart.get(other));
      });
    } else {
      allCharts.forEach(other => {
        if (other !== c) other.clearCrosshairPosition();
      });
    }
    syncingCrosshair = false;
  });
});

function clearAllCrosshairs() {
  allCharts.forEach(c => c.clearCrosshairPosition());
  updateIndicatorLegends(barsAscending.length - 1);
}

let bondHistory = [];
let bondValues = [];
let atrValues = [];
let rsiValues = [];
let rsiMaValues = [];
let rsiDiffValues = [];

function fmtIndicator(n) {
  return n === undefined || n === null || Number.isNaN(n) ? '–' : n.toFixed(2);
}

function updateIndicatorLegends(idx) {
  const bondEl = document.getElementById('legend-bond');
  const atrEl = document.getElementById('legend-atr');
  const rsiEl = document.getElementById('legend-rsi');
  const rsiMaEl = document.getElementById('legend-rsi-ma');
  const rsiDiffEl = document.getElementById('legend-rsi-diff');
  if (idx === null || idx === undefined) {
    bondEl.textContent = '–';
    atrEl.textContent = '–';
    rsiEl.textContent = '–';
    rsiMaEl.textContent = '–';
    rsiDiffEl.textContent = '–';
    rsiDiffEl.style.color = '';
    return;
  }
  bondEl.textContent = bondValues[idx] == null ? '–' : `${fmtIndicator(bondValues[idx])}%`;
  atrEl.textContent = fmtIndicator(atrValues[idx]);
  rsiEl.textContent = fmtIndicator(rsiValues[idx]);
  rsiMaEl.textContent = fmtIndicator(rsiMaValues[idx]);
  const diff = rsiDiffValues[idx];
  rsiDiffEl.textContent = fmtIndicator(diff);
  rsiDiffEl.style.color = diff == null ? '' : diff >= 0 ? '#26a69a' : '#ef5350';
}

function updateAITheme(theme) {
  const el = document.getElementById('ai-theme');
  el.className = theme ? 'theme-pill' : '';
  el.textContent = theme || 'n/a';
}

chart.subscribeCrosshairMove(param => {
  if (!barsAscending.length) return;
  if (!param || !param.time) {
    updateIndicatorLegends(barsAscending.length - 1);
    return;
  }
  const idx = barTimeToIndex.get(param.time);
  updateIndicatorLegends(idx === undefined ? null : idx);
});

function rma(values, length) {
  const out = new Array(values.length).fill(null);
  let sum = 0;
  for (let i = 0; i < values.length; i++) {
    sum += values[i];
    if (i === length - 1) {
      out[i] = sum / length;
    } else if (i >= length) {
      out[i] = (values[i] - out[i - 1]) / length + out[i - 1];
    }
  }
  return out;
}

function smaSkipNulls(values, length) {
  const out = new Array(values.length).fill(null);
  const buffer = [];
  for (let i = 0; i < values.length; i++) {
    const v = values[i];
    if (v === null || v === undefined) continue;
    buffer.push(v);
    if (buffer.length > length) buffer.shift();
    if (buffer.length === length) {
      out[i] = buffer.reduce((a, b) => a + b, 0) / length;
    }
  }
  return out;
}

function computeATR(bars, length = 14) {
  const tr = bars.map((b, i) => {
    if (i === 0) return b.high - b.low;
    const prevClose = bars[i - 1].close;
    return Math.max(b.high - b.low, Math.abs(b.high - prevClose), Math.abs(b.low - prevClose));
  });
  return rma(tr, length);
}

function computeRSI(bars, length = 14) {
  const gains = new Array(bars.length).fill(0);
  const losses = new Array(bars.length).fill(0);
  for (let i = 1; i < bars.length; i++) {
    const change = bars[i].close - bars[i - 1].close;
    gains[i] = Math.max(change, 0);
    losses[i] = Math.max(-change, 0);
  }
  const avgGain = rma(gains, length);
  const avgLoss = rma(losses, length);
  return bars.map((_, i) => {
    if (avgGain[i] === null || avgLoss[i] === null) return null;
    if (avgLoss[i] === 0) return avgGain[i] === 0 ? 0 : 100;
    return 100 - 100 / (1 + avgGain[i] / avgLoss[i]);
  });
}

function toSeriesData(bars, values) {
  return bars.map((b, i) =>
    values[i] === null || values[i] === undefined ? { time: b.time } : { time: b.time, value: values[i] }
  );
}

function barDate(time) {
  return new Date(time * 1000).toISOString().slice(0, 10);
}

function updateBondPane() {
  let j = 0;
  let last = null;
  bondValues = barsAscending.map(b => {
    const date = barDate(b.time);
    while (j < bondHistory.length && bondHistory[j].date <= date) {
      last = bondHistory[j].yieldPct;
      j++;
    }
    return last;
  });
  bondSeries.setData(toSeriesData(barsAscending, bondValues));
  alignBondChart();
}

function updateIndicators() {
  atrValues = computeATR(barsAscending, 14);
  rsiValues = computeRSI(barsAscending, 14);
  rsiMaValues = smaSkipNulls(rsiValues, 14);
  rsiDiffValues = barsAscending.map((_, i) =>
    rsiValues[i] === null || rsiMaValues[i] === null ? null : rsiValues[i] - rsiMaValues[i]
  );

  updateBondPane();
  atrSeries.setData(toSeriesData(barsAscending, atrValues));
  rsiSeries.setData(toSeriesData(barsAscending, rsiValues));
  rsiMaSeries.setData(toSeriesData(barsAscending, rsiMaValues));
  rsiDiffSeries.setData(toSeriesData(barsAscending, rsiDiffValues));

  updateIndicatorLegends(barsAscending.length - 1);
}

new ResizeObserver(entries => {
  for (const entry of entries) {
    bondChart.applyOptions({ width: entry.contentRect.width, height: entry.contentRect.height });
  }
  alignBondChart();
}).observe(bondWrapEl);

new ResizeObserver(entries => {
  for (const entry of entries) {
    atrChart.applyOptions({ width: entry.contentRect.width, height: entry.contentRect.height });
  }
}).observe(document.getElementById('atr-wrap'));

new ResizeObserver(entries => {
  for (const entry of entries) {
    rsiChart.applyOptions({ width: entry.contentRect.width, height: entry.contentRect.height });
  }
}).observe(document.getElementById('rsi-wrap'));

new ResizeObserver(entries => {
  for (const entry of entries) {
    rsiDiffChart.applyOptions({ width: entry.contentRect.width, height: entry.contentRect.height });
  }
}).observe(document.getElementById('rsi-diff-wrap'));

let equilibriumLine = null;
let currentZones = null;
let barsAscending = [];
let barTimeToIndex = new Map();
let currentSymbol = '';
let zoneStartTime = null;
let zoneEndTime = null;

new ResizeObserver(entries => {
  for (const entry of entries) {
    chart.applyOptions({ width: entry.contentRect.width, height: entry.contentRect.height });
  }
  scheduleZoneBoxUpdate();
}).observe(chartWrapEl);

chart.timeScale().subscribeVisibleLogicalRangeChange(() => updateZoneBoxes());

function positionBox(el, topPrice, bottomPrice, visible) {
  const xStart = zoneStartTime === null ? null : chart.timeScale().timeToCoordinate(zoneStartTime);
  const xEnd = zoneEndTime === null ? null : chart.timeScale().timeToCoordinate(zoneEndTime);
  const width = chartEl.clientWidth - PRICE_SCALE_WIDTH;
  const left = Math.max(0, xStart === null ? 0 : xStart);
  const right = Math.min(width, xEnd === null ? width : xEnd);
  if (right <= left) visible = false;
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
  el.style.left = `${left}px`;
  el.style.width = `${right - left}px`;
  el.style.right = 'auto';
  el.style.top = `${Math.min(yTop, yBottom)}px`;
  el.style.height = `${Math.max(1, Math.abs(yBottom - yTop))}px`;
}

function updateZoneBoxes() {
  if (!currentZones) return;
  const z = currentZones;
  positionBox(supplyBoxEl, z.supplyTop, z.supplyBtm, z.supplyReached);
  positionBox(demandBoxEl, z.demandTop, z.demandBtm, z.demandReached);
}

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
  const operatingMarginEl = document.getElementById('fund-operating-margin');

  if (f && f.peRatio != null) {
    peEl.textContent = f.peRatio.toFixed(2);
  } else if (f && f.forwardPERatio != null) {
    peEl.textContent = `${f.forwardPERatio.toFixed(2)} (fwd)`;
  } else {
    peEl.textContent = 'n/a';
  }
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

  if (f && f.operatingMarginPct != null) {
    operatingMarginEl.textContent = `${f.operatingMarginPct.toFixed(2)}%`;
    operatingMarginEl.style.color = f.operatingMarginPct >= 0 ? '#26a69a' : '#ef5350';
  } else {
    operatingMarginEl.textContent = 'n/a';
    operatingMarginEl.style.color = '';
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
  const resp = await fetch(`/api/data?symbol=${encodeURIComponent(currentSymbol)}`);
  if (!resp.ok) {
    statusEl.textContent = `Error: ${await resp.text()}`;
    return;
  }
  const data = await resp.json();
  const symbolLabel = document.getElementById('symbol-label');
  symbolLabel.textContent = data.symbol;
  symbolLabel.href = `https://www.tradingview.com/chart/?symbol=${encodeURIComponent(data.symbol)}`;
  document.getElementById('symbol-switcher-text').textContent = data.symbol;
  currentSymbol = data.symbol;

  barsAscending = data.bars;
  const zoneFirstIdx = Math.max(0, barsAscending.length - (data.lookbackBars || barsAscending.length));
  zoneStartTime = barsAscending.length ? barsAscending[zoneFirstIdx].time : null;
  zoneEndTime = barsAscending.length ? barsAscending[barsAscending.length - 1].time : null;
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

  updateIndicators();

  updateLegend(barsAscending[barsAscending.length - 1], barsAscending[barsAscending.length - 2]);
  updateFundamentals(data.fundamentals);
  updateAITheme(data.aiTheme);

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

function updateAllRangePerformance() {
  if (!barsAscending.length) return;
  const lastBar = barsAscending[barsAscending.length - 1];
  for (const key of Object.keys(RANGE_DAYS)) {
    setPctLabel(key, findStartBarForDays(RANGE_DAYS[key]), lastBar);
  }
}

function resetZoom() {
  if (!barsAscending.length) return;
  if (activeRange) {
    applyRange(activeRange);
  } else {
    chart.timeScale().fitContent();
    clearAllCrosshairs();
    scheduleZoneBoxUpdate();
  }
}

function applyRange(rangeKey) {
  if (!barsAscending.length) return;
  const lastBar = barsAscending[barsAscending.length - 1];
  const startBar = findStartBarForDays(RANGE_DAYS[rangeKey]);

  chart.timeScale().setVisibleRange({ from: startBar.time, to: lastBar.time });
  clearAllCrosshairs();

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
  const pctCell = n => n != null ? `<span style="color:${n >= 0 ? '#26a69a' : '#ef5350'}">${n.toFixed(2)}%</span>` : 'n/a';
  const rows = peers.map(p => `
    <tr class="${p.symbol === symbol ? 'highlight-row' : ''}">
      <td>${p.symbol}${p.symbol === symbol ? ' (this)' : ''}</td>
      <td>${p.trailingPE != null ? fmt(p.trailingPE) : 'n/a'}</td>
      <td>${p.forwardPE != null ? fmt(p.forwardPE) : 'n/a'}</td>
      <td>${p.pegRatio != null ? fmt(p.pegRatio) : 'n/a'}</td>
      <td>${p.psRatio != null ? fmt(p.psRatio) : 'n/a'}</td>
      <td>${pctCell(p.roicPct)}</td>
      <td>${pctCell(p.revenueGrowthPct)}</td>
      <td>${pctCell(p.netMarginPct)}</td>
      <td>${fmtMoney(p.marketCap)}</td>
    </tr>
  `).join('');
  el.innerHTML = `<table><tr><th>Symbol</th><th>P/E</th><th>Fwd P/E</th><th>PEG</th><th>P/S</th><th>ROIC TTM</th><th>Revenue Growth (YoY)</th><th>Net Margin</th><th>Mkt Cap</th></tr>${rows}</table>`;
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
    <div class="summary-text">${profile.summary || ''}</div>
  `;
}

function renderInsiderTransactions(ownership) {
  const el = document.getElementById('insider-body');
  if (!ownership) {
    el.innerHTML = '<span class="empty-note">No ownership data available.</span>';
    return;
  }
  const insiderRows = (ownership.insiderTransactions || []).slice(0, 8).map(t => {
    const info = insiderTypeInfo(t.text);
    return `
      <tr>
        <td>${t.filerName || 'n/a'}</td>
        <td>${t.relation || 'n/a'}</td>
        <td style="color:${info.color};font-weight:600;" title="${t.text || ''}">${info.label}</td>
        <td>${t.shares != null ? Math.round(t.shares).toLocaleString() : 'n/a'}</td>
        <td>${t.date || 'n/a'}</td>
      </tr>
    `;
  }).join('') || '<tr><td colspan="5" class="empty-note">n/a</td></tr>';

  el.innerHTML = `<table><tr><th>Filer</th><th>Relation</th><th>Type</th><th>Shares</th><th>Date</th></tr>${insiderRows}</table>`;
}

function insiderTypeInfo(text) {
  if (!text) return { label: 'Unknown', color: '' };
  const lower = text.toLowerCase();
  if (lower.startsWith('sale')) return { label: 'Sell', color: '#ef5350' };
  if (lower.startsWith('purchase') || lower.startsWith('buy')) return { label: 'Buy', color: '#26a69a' };
  if (lower.startsWith('conversion') || lower.startsWith('exercise')) return { label: 'Exercise', color: '' };
  if (lower.startsWith('grant') || lower.startsWith('award')) return { label: 'Grant', color: '' };
  return { label: text.split(' ')[0], color: '' };
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
  const net = ownership.netInsiderActivity || {};

  el.innerHTML = `
    <div class="ownership-grid">
      <div>
        <div class="stat-row"><span>Insiders Hold</span><b>${fmtFractionPct(ownership.insidersPct)}</b></div>
        <div class="stat-row"><span>Institutions Hold</span><b>${fmtFractionPct(ownership.institutionsPct)}</b></div>
        <div class="stat-row"><span>Institution Count</span><b>${ownership.institutionsCount != null ? Math.round(ownership.institutionsCount).toLocaleString() : 'n/a'}</b></div>
        <div class="stat-row"><span>Net Insider Shares (${net.period || 'n/a'})</span><b>${net.netShares != null ? Math.round(net.netShares).toLocaleString() : 'n/a'}</b></div>
      </div>
      <div>
        <h4 class="card-subheading">Top Institutional Holders</h4>
        <table><tr><th>Holder</th><th>% Held</th><th>Value</th></tr>${holderRows(ownership.topInstitutions)}</table>
      </div>
      <div>
        <h4 class="card-subheading">Top Fund Holders</h4>
        <table><tr><th>Fund</th><th>% Held</th><th>Value</th></tr>${holderRows(ownership.topFunds)}</table>
      </div>
      <div>
        <h4 class="card-subheading">Company Profile</h4>
        <div id="profile-body"><span class="empty-note">Loading…</span></div>
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
    <h4 class="card-subheading spaced">Latest Balance Sheet &amp; Cash Flow</h4>
    <div class="stat-row"><span>Total Cash</span><b>${fmtMoney(balanceSheet && balanceSheet.totalCash)}</b></div>
    <div class="stat-row"><span>Total Debt</span><b>${fmtMoney(balanceSheet && balanceSheet.totalDebt)}</b></div>
    <div class="stat-row"><span>Current Ratio</span><b>${balanceSheet && balanceSheet.currentRatio != null ? fmt(balanceSheet.currentRatio) : 'n/a'}</b></div>
    <div class="stat-row"><span>Quick Ratio</span><b>${balanceSheet && balanceSheet.quickRatio != null ? fmt(balanceSheet.quickRatio) : 'n/a'}</b></div>
    <div class="stat-row"><span>Operating Cash Flow</span><b>${fmtMoney(cashFlow && cashFlow.operatingCashflow)}</b></div>
    <div class="stat-row"><span>Free Cash Flow</span><b>${fmtMoney(cashFlow && cashFlow.freeCashflow)}</b></div>
    <div class="empty-note" style="margin-top:var(--space-3); display:block;">Balance sheet &amp; cash flow are latest-available snapshots — Yahoo's free API no longer provides historical detail for these.</div>
  `;
}

function renderFilings(filings) {
  const el = document.getElementById('filings-body');
  if (!filings || !filings.length) {
    el.innerHTML = '<span class="empty-note">No filings available.</span>';
    return;
  }
  el.innerHTML = filings.slice(0, 12).map(f => `
    <div class="filing-row">
      <span class="filing-type">${f.type || ''}</span>
      <span class="filing-date">${f.date || ''}</span>
      <a class="filing-title" href="${f.url}" target="_blank" rel="noopener">${f.title || f.url}</a>
    </div>
  `).join('');
}

function yieldClass(y) {
  return y == null ? '' : y >= 15 ? 'yield-distress' : y >= 10 ? 'yield-high' : '';
}

function fmtBp(change) {
  if (change == null) return 'n/a';
  const bp = Math.round(change * 100);
  return `<span style="color:${bp > 0 ? '#ef5350' : bp < 0 ? '#26a69a' : 'inherit'}">${bp > 0 ? '+' : ''}${bp} bp</span>`;
}

function bondLabel(b) {
  return `${b.couponPct != null ? Number(b.couponPct.toFixed(3)) : '?'}% ${b.maturity || ''}`;
}

function setBondPane(visible, label) {
  bondWrapEl.classList.toggle('hidden', !visible);
  document.getElementById('legend-bond-name').innerHTML = label;
}

function clearBondPane(visible, label) {
  bondHistory = [];
  updateBondPane();
  updateIndicatorLegends(barsAscending.length - 1);
  setBondPane(visible, label);
}

async function loadBondHistory(symbol, bond) {
  document.querySelectorAll('#bonds-body tr[data-bond]').forEach(tr => tr.classList.toggle('selected-row', tr.dataset.bond === bond));
  setBondPane(true, 'Bond Yield · loading…');
  try {
    const resp = await fetch(`/api/bonds/history?symbol=${encodeURIComponent(symbol)}&bond=${encodeURIComponent(bond)}`);
    if (symbol !== currentSymbol) return;
    if (!resp.ok) {
      clearBondPane(true, `Bond Yield · unavailable: ${await resp.text()}`);
      return;
    }
    const data = await resp.json();
    if (!data.history || !data.history.length) {
      clearBondPane(true, `Bond Yield ${data.label} · no trades in the last two years`);
      return;
    }
    bondHistory = data.history;
    updateBondPane();
    updateIndicatorLegends(barsAscending.length - 1);
    setBondPane(true, `Bond Yield ${data.label}`);
  } catch (e) {
    console.error('loading bond history failed', e);
  }
}

function renderBonds(data) {
  const el = document.getElementById('bonds-body');
  const bonds = data.bonds || [];
  if (!bonds.length) {
    el.innerHTML = '<span class="empty-note">No bonds found in FINRA TRACE for this ticker.</span>';
    clearBondPane(false, 'Bond Yield');
    return;
  }
  const headline = bonds.find(b => b.symbol === data.headline);
  const summary = headline
    ? `<div class="bonds-summary">Highest straight-bond yield: <b class="${yieldClass(headline.yieldPct)}">${fmt(headline.yieldPct)}%</b> on the ${bondLabel(headline)} (${fmtBp(headline.yieldChange)} on the last trade)</div>`
    : '<div class="bonds-summary">No straight bond with a recent trade.</div>';
  const rows = bonds.map(b => `
    <tr data-bond="${b.symbol}" class="${b.symbol === data.headline ? 'highlight-row' : ''}" title="${b.issuer || ''} · CUSIP ${b.cusip || 'n/a'} · click to show its yield under the price chart">
      <td>${bondLabel(b)}${b.convertible ? '<span class="bond-tag">Convertible</span>' : ''}${b.callable ? '<span class="bond-tag">Callable</span>' : ''}</td>
      <td>${[b.moodys, b.sp].filter(Boolean).join(' / ') || 'n/a'}</td>
      <td>${fmt(b.price)}</td>
      <td>${b.priceChange != null ? `<span style="color:${b.priceChange >= 0 ? '#26a69a' : '#ef5350'}">${b.priceChange >= 0 ? '+' : ''}${fmt(b.priceChange)}</span>` : 'n/a'}</td>
      <td class="${yieldClass(b.convertible ? null : b.yieldPct)}">${b.yieldPct != null ? `${fmt(b.yieldPct)}%` : 'n/a'}</td>
      <td>${fmtBp(b.yieldChange)}</td>
      <td>${b.lastTrade || 'n/a'}</td>
    </tr>
  `).join('');
  el.innerHTML = `${summary}<div class="bonds-scroll"><table><tr><th>Bond</th><th>Rating (Moody's / S&amp;P)</th><th>Price</th><th>Change</th><th>Yield</th><th>Yield Change</th><th>Last Trade</th></tr>${rows}</table></div>`;
  el.querySelectorAll('tr[data-bond]').forEach(tr => tr.addEventListener('click', () => loadBondHistory(data.symbol, tr.dataset.bond)));
  const first = headline || bonds.find(b => b.lastTrade) || bonds[0];
  loadBondHistory(data.symbol, first.symbol);
}

async function loadBonds() {
  const symbol = currentSymbol;
  const el = document.getElementById('bonds-body');
  try {
    const resp = await fetch(`/api/bonds?symbol=${encodeURIComponent(symbol)}`);
    if (symbol !== currentSymbol) return;
    if (!resp.ok) {
      el.innerHTML = `<span class="empty-note">Unavailable: ${await resp.text()}</span>`;
      clearBondPane(false, 'Bond Yield');
      return;
    }
    renderBonds(await resp.json());
  } catch (e) {
    console.error('loading bonds failed', e);
  }
}

async function loadCompanyData() {
  loadBonds();
  const insiderHref = `insider.html?symbol=${encodeURIComponent(currentSymbol)}`;
  document.getElementById('insider-link').href = insiderHref;
  document.getElementById('insider-all-link').href = insiderHref;
  document.getElementById('list-link').href = `list.html?symbol=${encodeURIComponent(currentSymbol)}`;
  try {
    const resp = await fetch(`/api/company?symbol=${encodeURIComponent(currentSymbol)}`);
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
    renderOwnership(data.ownership);
    renderProfile(data.profile);
    renderInsiderTransactions(data.ownership);
    renderFinancials(data.financials, data.balanceSheet, data.cashFlow);
    renderFilings(data.filings);
  } catch (e) {
    console.error('loading company data failed', e);
  }
}

document.getElementById('refresh').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  btn.disabled = true;
  btn.classList.add('spinning');
  statusEl.textContent = 'Refreshing from Yahoo Finance, OpenInsider and FINRA…';
  try {
    const resp = await fetch(`/api/refresh?symbol=${encodeURIComponent(currentSymbol)}`, { method: 'POST' });
    if (!resp.ok) {
      statusEl.textContent = `Refresh failed: ${await resp.text()}`;
      return;
    }
    await loadData();
    await loadCompanyData();
  } finally {
    btn.disabled = false;
    btn.classList.remove('spinning');
  }
});

let trackedSymbols = [];

async function switchSymbol(symbol) {
  if (symbol === currentSymbol) return;
  currentSymbol = symbol;
  activeRange = '2Y';
  const url = new URL(window.location);
  url.searchParams.set('symbol', symbol);
  window.history.replaceState({}, '', url);
  await loadData();
  await loadCompanyData();
}


const paletteOverlayEl = document.getElementById('palette-overlay');
const paletteInputEl = document.getElementById('palette-input');
const paletteListEl = document.getElementById('palette-list');
let paletteMatches = [];
let paletteActiveIndex = 0;

function openPalette() {
  paletteOverlayEl.classList.remove('hidden');
  paletteInputEl.value = '';
  renderPaletteMatches('');
  paletteInputEl.focus();
}

function closePalette() {
  paletteOverlayEl.classList.add('hidden');
}

const PALETTE_BROWSE_LIMIT = 50;

function renderPaletteMatches(query) {
  const q = query.trim().toLowerCase();
  paletteActiveIndex = 0;

  if (!q && trackedSymbols.length > PALETTE_BROWSE_LIMIT) {
    paletteMatches = [];
    paletteListEl.innerHTML = `<div class="palette-empty">Type to search ${trackedSymbols.length} tickers…</div>`;
    return;
  }

  paletteMatches = q
    ? trackedSymbols.filter(s => s.symbol.toLowerCase().includes(q) || s.name.toLowerCase().includes(q))
    : trackedSymbols;

  if (!paletteMatches.length) {
    paletteListEl.innerHTML = `<div class="palette-empty">No tracked ticker or company matches "${query}".</div>`;
    return;
  }

  paletteListEl.innerHTML = paletteMatches.map((s, i) => `
    <div class="palette-item${i === paletteActiveIndex ? ' active' : ''}" data-symbol="${s.symbol}">
      <span class="palette-main">
        <span class="palette-symbol">${s.symbol}</span>
        ${s.name ? `<span class="palette-name">${s.name}</span>` : ''}
      </span>
      ${s.symbol === currentSymbol ? '<span class="palette-hint">current</span>' : ''}
    </div>
  `).join('');
}

function setPaletteActive(index) {
  if (!paletteMatches.length) return;
  paletteActiveIndex = (index + paletteMatches.length) % paletteMatches.length;
  paletteListEl.querySelectorAll('.palette-item').forEach((el, i) => {
    el.classList.toggle('active', i === paletteActiveIndex);
  });
  const activeEl = paletteListEl.children[paletteActiveIndex];
  if (activeEl) activeEl.scrollIntoView({ block: 'nearest' });
}

function selectPaletteActive() {
  const match = paletteMatches[paletteActiveIndex];
  if (!match) return;
  closePalette();
  switchSymbol(match.symbol);
}

document.getElementById('symbol-switcher').addEventListener('click', openPalette);

paletteInputEl.addEventListener('input', () => renderPaletteMatches(paletteInputEl.value));

paletteInputEl.addEventListener('keydown', e => {
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    setPaletteActive(paletteActiveIndex + 1);
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    setPaletteActive(paletteActiveIndex - 1);
  } else if (e.key === 'Enter') {
    e.preventDefault();
    selectPaletteActive();
  } else if (e.key === 'Escape') {
    e.preventDefault();
    closePalette();
  }
});

paletteListEl.addEventListener('click', e => {
  const item = e.target.closest('.palette-item');
  if (!item) return;
  closePalette();
  switchSymbol(item.dataset.symbol);
});

paletteOverlayEl.addEventListener('click', e => {
  if (e.target === paletteOverlayEl) closePalette();
});

window.addEventListener('keydown', e => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
    e.preventDefault();
    if (paletteOverlayEl.classList.contains('hidden')) {
      openPalette();
    } else {
      closePalette();
    }
  }
});


const WHEEL_CYCLE_COOLDOWN_MS = 350;
let lastWheelCycleAt = 0;

function cycleSymbol(direction) {
  if (!trackedSymbols.length) return;
  const idx = trackedSymbols.findIndex(s => s.symbol === currentSymbol);
  const nextIdx = ((idx === -1 ? 0 : idx) + direction + trackedSymbols.length) % trackedSymbols.length;
  switchSymbol(trackedSymbols[nextIdx].symbol);
}

function onShiftWheelCycle(e) {
  if (!e.shiftKey) return;
  e.preventDefault();
  const now = Date.now();
  if (now - lastWheelCycleAt < WHEEL_CYCLE_COOLDOWN_MS) return;
  lastWheelCycleAt = now;
  cycleSymbol(e.deltaY > 0 ? 1 : -1);
}

chartWrapEl.addEventListener('wheel', onShiftWheelCycle, { passive: false });

async function init() {
  const resp = await fetch('/api/symbols');
  const data = await resp.json();
  trackedSymbols = data.symbols || [];

  const requested = new URLSearchParams(window.location.search).get('symbol');
  const requestedValid = requested && trackedSymbols.some(s => s.symbol === requested);
  currentSymbol = requestedValid ? requested : (trackedSymbols[0] && trackedSymbols[0].symbol) || '';

  await loadData();
  await loadCompanyData();
}

init();
