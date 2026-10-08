const fs = require('node:fs/promises');
const path = require('node:path');
const { parseArgs } = require('node:util');
const { setTimeout: delay } = require('node:timers/promises');
const { validateURL, automaticLogin, waitForRatings } = require('./fetch.cjs');

const FOLDED = { 'ß': 'ss', 'æ': 'ae', 'œ': 'oe', 'þ': 'th', 'ð': 'd', 'đ': 'd', 'ħ': 'h', 'ŧ': 't', 'ø': 'o', 'ł': 'l' };

const ACCESS_EXIT_CODE = 3;
const MAX_CANDIDATES = 10;

class AccessError extends Error {}

function normalizeName(value) {
  const words = value.normalize('NFKD').toLowerCase().replace(/\p{M}/gu, '').replace(/[ßæœþðđħŧøł]/g, char => FOLDED[char])
    .replace(/[’']/g, '').replace(/&/g, ' and ').replace(/[^a-z0-9]+/g, ' ').trim().split(/\s+/).filter(Boolean)
    .filter(word => !['inc', 'incorporated', 'corp', 'corporation', 'co', 'company', 'ltd', 'limited', 'plc'].includes(word));
  if (words.length > 1 && words[0] === 'the') words.shift();
  return words.join(' ');
}

function selectEntity(links, name) {
  const expected = normalizeName(name);
  if (!expected) return { status: 'needs_name' };
  const matches = new Map();
  const listed = new Map();
  for (const link of links) {
    let url;
    try { url = validateURL(link.url); } catch { continue; }
    const candidate = { url, name: link.name };
    listed.set(url, candidate);
    if (normalizeName(link.name) === expected) matches.set(url, candidate);
  }
  const candidates = list => [...list.values()].slice(0, MAX_CANDIDATES);
  if (matches.size > 1) return { status: 'ambiguous', candidates: candidates(matches) };
  if (matches.size === 1) return { status: 'verified', ...matches.values().next().value };
  return { status: 'unmatched', candidates: candidates(listed) };
}

async function checkAccess(page) {
  const text = await page.locator('body').innerText();
  if (/Security Controls Triggered|Access Denied|verify you are human|captcha/i.test(text)) {
    throw new AccessError('S&P requires browser verification; run credit-refresh with -headed -manual.');
  }
  if (new URL(page.url()).hostname !== 'www.spglobal.com') throw new AccessError('Complete S&P sign-in in the browser.');
  if (/\/login(?:[/?#]|$)/i.test(page.url())) throw new AccessError('S&P login expired; refresh the saved session.');
}

async function ensureAccess(page, options) {
  try {
    await checkAccess(page);
    return;
  } catch (error) {
    if (!options.manual) throw error;
    console.error('Complete S&P verification/sign-in in the open browser. The batch resumes automatically when access is available.');
  }
  const deadline = Date.now() + options.manualWait;
  while (Date.now() < deadline) {
    if (page.isClosed()) throw new AccessError('Browser closed during S&P verification.');
    await delay(500);
    try {
      await checkAccess(page);
      if (new URL(page.url()).hostname === 'www.spglobal.com' && (await page.locator('body').innerText()).trim()) return;
    } catch {}
  }
  throw new AccessError('S&P verification was not completed. Existing ratings were retained.');
}

async function discover(page, name, timeout, access = () => checkAccess(page)) {
  const term = name.replace(/\b(?:incorporated|corporation|limited|inc|corp|company|co|ltd|plc)\b\.?/gi, ' ').replace(/\s+/g, ' ').trim();
  if (!term) return { status: 'needs_name' };
  await page.goto('https://www.spglobal.com/ratings/en/regulatory/consolidated-search-entity/searchTerm/' + encodeURIComponent(term), { waitUntil: 'domcontentloaded', timeout });
  await access();
  await page.waitForFunction(() => document.querySelector('a[href*="/org-details/"]') || /no results|no records|no matches|0 results/i.test(document.body.innerText), null, { timeout });
  await access();
  const links = await page.locator('a[href*="/org-details/"]').evaluateAll(anchors => anchors.map(a => ({ url: a.href, name: a.innerText.trim() })));
  return selectEntity(links, name);
}

async function runBatch(request, emit, options) {
  const { chromium } = require('playwright');
  const browser = await chromium.launch({ headless: !options.headed && !options.manual, channel: options.channel || undefined });
  try {
    let storageState;
    if (options.session) {
      try { storageState = JSON.parse(await fs.readFile(options.session, 'utf8')); }
      catch (error) { if (error.code !== 'ENOENT') throw new Error('Cannot read S&P session file.'); }
    }
    const context = await browser.newContext({ storageState });
    context.setDefaultTimeout(options.timeout);
    const page = await context.newPage();
    const access = () => ensureAccess(page, options);
    const saveSession = async () => {
      if (!options.session) return;
      await fs.mkdir(path.dirname(path.resolve(options.session)), { recursive: true });
      await fs.writeFile(options.session, JSON.stringify(await context.storageState({ indexedDB: true })), { mode: 0o600 });
    };
    const loginURL = 'https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461';
    await page.goto(loginURL, { waitUntil: 'domcontentloaded', timeout: options.timeout });
    if (!options.manual && /\/login(?:[/?#]|$)/i.test(page.url())) {
      if (!await automaticLogin(page, true)) throw new AccessError('S&P login required; save a session using fetch.cjs --manual --session.');
      await page.waitForURL(url => !/\/login(?:[/?#]|$)/i.test(url.href), { timeout: options.timeout });
    }
    await access();
    await saveSession();
    for (const job of request.jobs) {
      try {
        const entity = job.url ? { status: 'verified', url: validateURL(job.url) } : await discover(page, job.name, options.timeout, access);
        if (entity.status !== 'verified') {
          emit({ symbol: job.symbol, status: entity.status, checkedAt: new Date().toISOString(), candidates: entity.candidates });
          continue;
        }
        await page.goto(entity.url, { waitUntil: 'domcontentloaded', timeout: options.timeout });
        await access();
        const data = await waitForRatings(page, options.timeout, entity.url);
        if (!data) throw new Error('No issuer ratings found; access, layout, or rating coverage needs checking.');
        if (normalizeName(data.entityName) !== normalizeName(job.name)) throw new Error('S&P issuer name does not match the tracked company.');
        emit({ symbol: job.symbol, status: 'verified', url: entity.url, name: data.entityName, checkedAt: new Date().toISOString(), data: {
          ...data, entityId: new URL(entity.url).pathname.match(/entityId\/(\d+)/)[1], sourceUrl: entity.url, fetchedAt: new Date().toISOString()
        } });
      } catch (error) {
        if (error instanceof AccessError) throw error;
        await access();
        emit({ symbol: job.symbol, status: 'error', error: error.message.split('\n')[0] });
      } finally {
        await delay(options.pacing);
      }
    }
    await saveSession();
  } finally {
    await browser.close();
  }
}

module.exports = { normalizeName, selectEntity, discover, checkAccess, ensureAccess, AccessError, ACCESS_EXIT_CODE };

if (require.main === module) {
  (async () => {
    const { values } = parseArgs({ options: { session: { type: 'string' }, channel: { type: 'string' }, timeout: { type: 'string', default: '30' }, pacing: { type: 'string', default: '2' }, headed: { type: 'boolean', default: false }, manual: { type: 'boolean', default: false }, 'manual-wait': { type: 'string', default: '180' } } });
    const options = { ...values, timeout: Number(values.timeout) * 1000, pacing: Number(values.pacing) * 1000, manualWait: Number(values['manual-wait']) * 1000 };
    if (!Number.isFinite(options.timeout) || options.timeout < 1000 || !Number.isFinite(options.pacing) || options.pacing < 0 || !Number.isFinite(options.manualWait) || options.manualWait < 1000) throw new Error('Invalid timeout, pacing, or verification wait.');
    let input = '';
    for await (const chunk of process.stdin) input += chunk;
    const request = JSON.parse(input);
    if (!Array.isArray(request.jobs)) throw new Error('Expected jobs array.');
    console.log = (...args) => process.stderr.write(args.join(' ') + '\n');
    await runBatch(request, item => process.stdout.write(JSON.stringify(item) + '\n'), options);
  })().catch(error => { console.error(error.message.split('\n')[0]); process.exitCode = error instanceof AccessError ? ACCESS_EXIT_CODE : 1; });
}
