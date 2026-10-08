const fs = require('node:fs/promises');
const path = require('node:path');
const { parseArgs } = require('node:util');
const { Writable } = require('node:stream');
const readline = require('node:readline/promises');
const { setTimeout: delay } = require('node:timers/promises');

const DEFAULT_URL = 'https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461';
const FIELDS = ['ratingType', 'rating', 'ratingDate', 'lastReviewDate', 'regulatoryIdentifiers', 'outlook', 'outlookDate'];
const HEADERS = ['ratingtype', 'rating', 'ratingdate', 'lastreviewdate', 'regulatoryidentifiers', 'creditwatchoutlook', 'creditwatchoutlookdate'];
const RATING = /^(?:AAA|AA[+-]?|A[+-]?|BBB[+-]?|BB[+-]?|B[+-]?|CCC[+-]?|CC|C|SD|D|NR|WR|A-[123][+]?|B-[123])$/;
const DATE = /^(?:\d{1,2}-[A-Za-z]{3}-\d{4}|N\/?A|[-–—])?$/;

function clean(value) {
  return String(value ?? '').replace(/Regulatory Disclosures/gi, '').replace(/\s+/g, ' ').trim();
}

function header(value) {
  return clean(value).toLowerCase().replace(/[^a-z]/g, '');
}

function ratingRow(cells) {
  if (cells.some(cell => cell === undefined || cell === null)) return null;
  const values = cells.map(clean);
  if (values.length !== 7 || !values[0] || !RATING.test(values[1])) return null;
  if (![2, 3, 6].every(index => DATE.test(values[index]))) return null;
  return Object.fromEntries(FIELDS.map((field, index) => [field, values[index]]));
}

function extractRatings(snapshot) {
  const ratings = [];
  for (const table of snapshot.tables) {
    const start = table.findIndex(row => HEADERS.every(name => row.some(cell => header(cell) === name)));
    if (start < 0) continue;
    const indices = HEADERS.map(name => table[start].findIndex(cell => header(cell) === name));
    for (const cells of table.slice(start + 1)) {
      const row = ratingRow(indices.map(index => cells[index]));
      if (row) ratings.push(row);
    }
  }
  if (!ratings.length) {
    const section = snapshot.text.split(/Issuer Credit Rating/i)[1]?.split(/View Ratings Definitions/i)[0];
    if (section) {
      const lines = section.split(/\r?\n/).map(clean).filter(Boolean);
      const starts = lines.flatMap((line, index) => /^(?:Local|Foreign) Currency (?:LT|ST)$/.test(line) ? [index] : []);
      for (let i = 0; i < starts.length; i++) {
        const cells = lines.slice(starts[i], starts[i + 1] ?? lines.length).slice(0, 7);
        const row = ratingRow(cells);
        if (row) ratings.push(row);
      }
    }
  }
  return [...new Map(ratings.map(row => [JSON.stringify(row), row])).values()];
}

function readDocument() {
  return {
    entityName: document.querySelector('h1')?.innerText.trim() || '',
    text: document.body.innerText,
    tables: [...document.querySelectorAll('table, [role="table"], [role="grid"]')].map(table =>
      [...table.querySelectorAll('tr, [role="row"]')].map(row =>
        [...row.querySelectorAll('th, td, [role="cell"], [role="columnheader"], [role="gridcell"]')].map(cell => cell.innerText)
      )
    )
  };
}

function validateURL(value) {
  const url = new URL(value);
  if (url.protocol !== 'https:' || url.hostname !== 'www.spglobal.com' || url.port || url.username || url.password ||
      !/^\/ratings\/en\/regulatory\/(?:entity\/)?org-details\/sectorCode\/[A-Z]+\/entityId\/\d+\/?$/.test(url.pathname)) {
    throw new Error('Use an https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/.../entityId/... URL.');
  }
  url.search = '';
  url.hash = '';
  return url.href;
}

function trustedLogin(frame) {
  try {
    const url = new URL(frame.url());
    return url.protocol === 'https:' && (url.hostname === 'spglobal.com' || url.hostname.endsWith('.spglobal.com'));
  } catch {
    return false;
  }
}

async function visibleField(page, selector, timeout = 10000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    for (const frame of page.frames().filter(trustedLogin)) {
      const fields = frame.locator(selector);
      for (let i = 0; i < await fields.count(); i++) {
        const field = fields.nth(i);
        if (await field.isVisible() && await field.isEditable()) return field;
      }
    }
    await delay(250);
  }
  return null;
}

async function submitLogin(page) {
  for (const frame of page.frames().filter(trustedLogin)) {
    const buttons = frame.getByRole('button', { name: /^(?:sign\s*in|log\s*in|continue|next|submit)$/i });
    for (let i = 0; i < await buttons.count(); i++) {
      if (await buttons.nth(i).isVisible()) {
        await buttons.nth(i).click();
        return true;
      }
    }
  }
  return false;
}

async function question(prompt, hidden = false) {
  if (!process.stdin.isTTY) throw new Error('Interactive login needs a terminal. Use SP_USERNAME/SP_PASSWORD or --manual in a terminal.');
  const output = hidden ? new Writable({ write(chunk, encoding, callback) { callback(); } }) : process.stdout;
  const rl = readline.createInterface({ input: process.stdin, output, terminal: true });
  if (hidden) process.stdout.write(prompt);
  try {
    return await rl.question(hidden ? '' : prompt);
  } finally {
    rl.close();
    if (hidden) process.stdout.write('\n');
  }
}

async function automaticLogin(page, headless) {
  const usernameField = await visibleField(page, 'input[type="email"], input[autocomplete="username"], input[name*="email" i], input[name*="username" i], input[id*="username" i]');
  if (!usernameField) return false;
  let username = process.env.SP_USERNAME;
  let password = process.env.SP_PASSWORD;
  if (headless && (!username || !password)) return false;
  username ||= await question('S&P email/username: ');
  password ||= await question('S&P password (hidden): ', true);
  if (!username || !password) return false;
  try {
    await usernameField.fill(username);
    let passwordField = await visibleField(page, 'input[type="password"]', 1000);
    if (!passwordField) {
      if (!await submitLogin(page)) return false;
      passwordField = await visibleField(page, 'input[type="password"]');
    }
    if (!passwordField) return false;
    await passwordField.fill(password);
    return await submitLogin(page);
  } catch {
    return false;
  } finally {
    password = undefined;
  }
}

async function waitForRatings(page, timeout, url) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    try {
      if (page.url().split(/[?#]/)[0].replace(/\/$/, '') !== url.replace(/\/$/, '')) {
        await delay(300);
        continue;
      }
      const snapshot = await page.evaluate(readDocument);
      const ratings = extractRatings(snapshot);
      if (ratings.length && snapshot.entityName) return { entityName: snapshot.entityName, ratings };
    } catch (error) {
      if (page.isClosed()) throw new Error('Browser was closed before the ratings could be read.');
      if (!/Execution context was destroyed|Cannot find context/.test(error.message)) throw error;
    }
    await delay(300);
  }
  return null;
}

function toCSV(result) {
  const fields = ['entityName', 'entityId', ...FIELDS, 'sourceUrl', 'fetchedAt'];
  const escape = value => '"' + String(value ?? '').replace(/"/g, '""') + '"';
  return [fields, ...result.ratings.map(row => fields.map(field => row[field] ?? result[field]))]
    .map(row => row.map(escape).join(',')).join('\r\n') + '\r\n';
}

async function main() {
  const { values } = parseArgs({ options: {
    url: { type: 'string', default: DEFAULT_URL },
    out: { type: 'string' },
    session: { type: 'string' },
    manual: { type: 'boolean', default: false },
    headless: { type: 'boolean', default: false },
    channel: { type: 'string' },
    timeout: { type: 'string', default: '30' },
    help: { type: 'boolean', short: 'h', default: false }
  } });
  if (values.help) {
    console.log('node fetch.cjs [--url URL] [--out FILE_PREFIX] [--manual] [--headless] [--channel chrome|msedge] [--session FILE] [--timeout SECONDS]\nCredentials: SP_USERNAME and SP_PASSWORD, or local terminal prompts. --session explicitly saves/reuses login state.');
    return;
  }
  const url = validateURL(values.url);
  const timeout = Number(values.timeout) * 1000;
  if (!Number.isFinite(timeout) || timeout < 1000) throw new Error('--timeout must be at least 1 second.');
  if (values.manual && values.headless) throw new Error('--manual requires a visible browser.');
  const { chromium } = require('playwright');
  const browser = await chromium.launch({ headless: values.headless, ...(values.channel ? { channel: values.channel } : {}) });
  try {
    let storageState;
    if (values.session) {
      try {
        storageState = JSON.parse(await fs.readFile(values.session, 'utf8'));
      } catch (error) {
        if (error.code !== 'ENOENT') throw new Error('Cannot read session file; remove it to sign in again.');
      }
    }
    const context = await browser.newContext({ storageState });
    context.setDefaultTimeout(timeout);
    const page = await context.newPage();
    await page.goto(url, { waitUntil: 'domcontentloaded', timeout });
    let data = await waitForRatings(page, timeout, url);
    if (!data && !values.manual) {
      console.log('Attempting S&P login.');
      if (await automaticLogin(page, values.headless)) {
        data = await waitForRatings(page, timeout, url);
        if (!data && !/\/login(?:[/?#]|$)/i.test(page.url())) {
          await page.goto(url, { waitUntil: 'domcontentloaded', timeout });
          data = await waitForRatings(page, timeout, url);
        }
      }
    }
    if (!data && !values.headless) {
      await question('Complete sign-in/MFA in the browser, then press Enter here: ');
      await page.goto(url, { waitUntil: 'domcontentloaded', timeout });
      data = await waitForRatings(page, timeout, url);
    }
    if (!data) throw new Error('No issuer ratings found. Check login/access, or rerun without --headless to complete login. The page layout may also have changed.');
    const result = {
      entityName: data.entityName,
      entityId: new URL(url).pathname.match(/entityId\/(\d+)/)[1],
      sourceUrl: url,
      fetchedAt: new Date().toISOString(),
      ratings: data.ratings
    };
    if (values.session) {
      await fs.mkdir(path.dirname(path.resolve(values.session)), { recursive: true });
      const state = await context.storageState({ indexedDB: true });
      await fs.writeFile(values.session, JSON.stringify(state), { mode: 0o600 });
    }
    const prefix = path.resolve(values.out || path.join(__dirname, 'output', result.entityId));
    await fs.mkdir(path.dirname(prefix), { recursive: true });
    await fs.writeFile(prefix + '.json', JSON.stringify(result, null, 2) + '\n');
    await fs.writeFile(prefix + '.csv', toCSV(result));
    console.log(`Fetched ${result.ratings.length} issuer ratings for ${result.entityName}.\n${prefix}.json\n${prefix}.csv`);
  } finally {
    await browser.close();
  }
}

module.exports = { extractRatings, readDocument, validateURL, toCSV, automaticLogin, waitForRatings, question };

if (require.main === module) {
  main().catch(error => {
    console.error(`Fetch failed: ${error.message}`);
    process.exitCode = 1;
  });
}
