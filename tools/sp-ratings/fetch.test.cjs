const test = require('node:test');
const assert = require('node:assert/strict');
const { extractRatings, readDocument, validateURL, toCSV } = require('./fetch.cjs');
const { selectEntity, discover, ensureAccess, normalizeName, AccessError, ACCESS_EXIT_CODE } = require('./batch.cjs');
const nameCases = require('./name-cases.json');

const headers = ['Rating Type', 'Rating', 'Rating Date', 'Last Review Date ?', 'Regulatory Identifiers', 'CreditWatch/ Outlook', 'CreditWatch/ Outlook Date'];
const local = ['Local Currency LT', 'BB\nRegulatory Disclosures', '25-Feb-2026', '25-Feb-2026', 'EE|UKE', 'Positive', '25-Feb-2026'];
const foreign = ['Foreign Currency LT', 'BB\nRegulatory Disclosures', '25-Feb-2026', '25-Feb-2026', 'EE|UKE', 'Positive', '25-Feb-2026'];

test('unattended verification stops and manual verification resumes only after access returns', async () => {
  let text = 'Security Controls Triggered';
  const page = { locator: () => ({ innerText: async () => text }), url: () => 'https://www.spglobal.com/ratings/en', isClosed: () => false };
  await assert.rejects(ensureAccess(page, {manual:false}), /verification/);
  const timer = setTimeout(() => {text = 'Entity Results';}, 30);
  try {
    await ensureAccess(page, {manual:true, manualWait:2000});
  } finally { clearTimeout(timer); }
  text = 'Security Controls Triggered';
  await assert.rejects(ensureAccess(page, {manual:true, manualWait:1}), /not completed/);
});

test('name normalization matches the shared fixtures used by the Go side', () => {
  for (const { input, want } of nameCases) assert.equal(normalizeName(input), want, input);
});

test('access failures are typed so the Go side can pause instead of charging companies', async () => {
  const page = { locator: () => ({ innerText: async () => 'Security Controls Triggered' }), url: () => 'https://www.spglobal.com/ratings/en', isClosed: () => false };
  await assert.rejects(ensureAccess(page, {manual:false}), AccessError);
  await assert.rejects(ensureAccess(page, {manual:true, manualWait:1}), AccessError);
  const login = { locator: () => ({ innerText: async () => 'Welcome' }), url: () => 'https://www.spglobal.com/login', isClosed: () => false };
  await assert.rejects(ensureAccess(login, {manual:false}), AccessError);
  assert.equal(ACCESS_EXIT_CODE, 3);
});

test('unmatched and ambiguous discovery keep candidates for manual resolution', () => {
  const base = 'https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/';
  const links = Array.from({length: 12}, (_, i) => ({url: base + (i + 1), name: 'Other ' + i}));
  const unmatched = selectEntity([...links, {url:'https://evil.test/x', name:'Bad'}], 'Grab Holdings Limited');
  assert.equal(unmatched.status, 'unmatched');
  assert.equal(unmatched.candidates.length, 10);
  assert.deepEqual(unmatched.candidates[0], {url: base + '1', name: 'Other 0'});
  const ambiguous = selectEntity([{url: base + '1', name:'Grab Holdings Ltd.'}, {url: base + '2', name:'Grab Holdings Inc'}, {url: base + '3', name:'Grab Financial'}], 'Grab Holdings Limited');
  assert.equal(ambiguous.status, 'ambiguous');
  assert.deepEqual(ambiguous.candidates.map(c => c.url), [base + '1', base + '2']);
  assert.equal(selectEntity([{url: base + '1', name:'Grab Holdings Ltd.'}], 'Grab Holdings Limited').candidates, undefined);
});

test('discovery matches accented and article-prefixed issuer names', () => {
  const url = 'https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/1';
  assert.equal(selectEntity([{url, name:'Nestle S.A.'}], 'Nestlé S.A.').status, 'verified');
  assert.equal(selectEntity([{url, name:'Coca-Cola Co'}], 'The Coca-Cola Company').status, 'verified');
});

test('discovery requires an exact normalized issuer name and rejects subsidiaries and ambiguity', () => {
  const url = 'https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461';
  assert.equal(selectEntity([{url, name:'Grab Holdings Ltd.'}], 'Grab Holdings Limited').status, 'verified');
  assert.equal(selectEntity([{url, name:'Grab Financial Services Ltd.'}], 'Grab Holdings Limited').status, 'unmatched');
  assert.equal(selectEntity([{url, name:'Grab Holdings Ltd.'}, {url:url.replace('682461','123'), name:'Grab Holdings Ltd.'}], 'Grab Holdings Limited').status, 'ambiguous');
  assert.equal(selectEntity([{url:url.replace('www.spglobal.com','evil.test'), name:'Grab Holdings Ltd.'}], 'Grab Holdings Limited').status, 'unmatched');
});

test('extracts the screenshot rows and removes disclosure labels', () => {
  const ratings = extractRatings({ tables: [[headers, local, foreign]], text: '' });
  assert.deepEqual(ratings, [
    { ratingType: 'Local Currency LT', rating: 'BB', ratingDate: '25-Feb-2026', lastReviewDate: '25-Feb-2026', regulatoryIdentifiers: 'EE|UKE', outlook: 'Positive', outlookDate: '25-Feb-2026' },
    { ratingType: 'Foreign Currency LT', rating: 'BB', ratingDate: '25-Feb-2026', lastReviewDate: '25-Feb-2026', regulatoryIdentifiers: 'EE|UKE', outlook: 'Positive', outlookDate: '25-Feb-2026' }
  ]);
});

test('maps reordered columns and deduplicates responsive tables', () => {
  const table = [headers.toReversed(), [...local].toReversed()];
  const rows = extractRatings({ tables: [table, table], text: '' });
  assert.equal(rows.length, 1);
  assert.equal(rows[0].rating, 'BB');
  assert.equal(rows[0].outlook, 'Positive');
});

test('reads a non-table layout bounded by the issuer section', () => {
  const text = ['Grab Holdings Ltd.', 'Issuer Credit Rating', ...headers, ...local, ...foreign, 'View Ratings Definitions', 'Unrelated content'].join('\n');
  assert.deepEqual(extractRatings({ tables: [], text }), extractRatings({ tables: [[headers, local, foreign]], text: '' }));
});

test('rejects login pages, incomplete rows, and malformed dates', () => {
  assert.deepEqual(extractRatings({ tables: [], text: 'S&P Global Ratings\nSign In' }), []);
  assert.deepEqual(extractRatings({ tables: [[headers, local.slice(0, 2)]], text: '' }), []);
  const wrongDate = [...local];
  wrongDate[2] = 'Subscribe';
  assert.deepEqual(extractRatings({ tables: [[headers, wrongDate]], text: '' }), []);
});

test('preserves signed ratings and empty date cells', () => {
  const cells = [...local];
  cells[1] = 'BBB-';
  cells[6] = '';
  const [row] = extractRatings({ tables: [[headers, cells]], text: '' });
  assert.equal(row.rating, 'BBB-');
  assert.equal(row.outlookDate, '');
});

test('restricts target URLs to S&P organization pages', () => {
  const url = 'https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/682461';
  assert.equal(validateURL(url + '?unused=true#heading'), url);
  for (const value of [url.replace('https:', 'http:'), url.replace('www.spglobal.com', 'www.spglobal.com.evil.test'), url.replace('www.spglobal.com', 'user:password@www.spglobal.com'), 'https://www.spglobal.com/ratings/login']) {
    assert.throws(() => validateURL(value));
  }
});

test('exports quoted CSV with provenance and escaped company names', () => {
  const csv = toCSV({ entityName: 'Example, "Holdings"', entityId: '682461', sourceUrl: 'https://www.spglobal.com/', fetchedAt: '2026-10-08T00:00:00.000Z', ratings: extractRatings({ tables: [[headers, local]], text: '' }) });
  assert.match(csv, /"Example, ""Holdings""","682461","Local Currency LT","BB"/);
  assert.match(csv, /"sourceUrl","fetchedAt"\r\n/);
});

test('reads rendered HTML and ARIA tables in a real browser', async () => {
  const { chromium } = require('playwright');
  const browser = await chromium.launch({ headless: true, ...(process.env.SP_TEST_CHANNEL ? { channel: process.env.SP_TEST_CHANNEL } : {}) });
  try {
    const page = await browser.newPage();
    const tr = (row, tag) => `<tr>${row.map(cell => `<${tag}>${cell.replace(/\n/g, '<br>')}</${tag}>`).join('')}</tr>`;
    await page.setContent(`<h1>Grab Holdings Ltd.</h1><h2>Issuer Credit Rating</h2><table>${tr(headers, 'th')}${tr(local, 'td')}${tr(foreign, 'td')}</table>`);
    let snapshot = await page.evaluate(readDocument);
    assert.equal(snapshot.entityName, 'Grab Holdings Ltd.');
    assert.equal(extractRatings(snapshot).length, 2);
    const ariaRow = (row, role) => `<div role="row">${row.map(cell => `<div role="${role}">${cell.replace(/\n/g, '<br>')}</div>`).join('')}</div>`;
    await page.setContent(`<h1>Grab Holdings Ltd.</h1><div role="table">${ariaRow(headers, 'columnheader')}${ariaRow(local, 'cell')}</div>`);
    snapshot = await page.evaluate(readDocument);
    assert.equal(extractRatings(snapshot)[0].regulatoryIdentifiers, 'EE|UKE');
    await page.route('https://www.spglobal.com/**', route => route.fulfill({ contentType:'text/html', body:'<h1>Entity Results</h1><a href="/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/689595">Apple Bidco, LLC</a><a href="/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/112354">Apple Inc.</a>' }));
    const entity = await discover(page, 'Apple Inc.', 1000);
    assert.equal(page.url(), 'https://www.spglobal.com/ratings/en/regulatory/consolidated-search-entity/searchTerm/Apple');
    assert.equal(entity.url, 'https://www.spglobal.com/ratings/en/regulatory/org-details/sectorCode/CORP/entityId/112354');
    assert.equal(entity.status, 'verified');
  } finally {
    await browser.close();
  }
});
