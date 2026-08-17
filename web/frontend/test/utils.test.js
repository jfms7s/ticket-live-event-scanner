const { test } = require('node:test');
const assert = require('node:assert');
const {
  getLatestNotificationStatus,
  formatDate,
  escapeHtml,
} = require('../utils.js');

test('getLatestNotificationStatus - returns "none" for empty notifications array', () => {
  const result = getLatestNotificationStatus([]);
  assert.strictEqual(result.status, 'none');
  assert.strictEqual(result.notif, null);
});

test('getLatestNotificationStatus - returns "none" for null notifications', () => {
  const result = getLatestNotificationStatus(null);
  assert.strictEqual(result.status, 'none');
  assert.strictEqual(result.notif, null);
});

test('getLatestNotificationStatus - returns first notification status', () => {
  const notifications = [
    { id: 1, status: 'sent', attempted_at: '2026-08-17T10:00:00Z' },
    { id: 2, status: 'pending', attempted_at: '2026-08-17T09:00:00Z' },
  ];
  const result = getLatestNotificationStatus(notifications);
  assert.strictEqual(result.status, 'sent');
  assert.deepStrictEqual(result.notif, notifications[0]);
});

test('getLatestNotificationStatus - handles single notification', () => {
  const notifications = [{ id: 1, status: 'failed' }];
  const result = getLatestNotificationStatus(notifications);
  assert.strictEqual(result.status, 'failed');
});

test('formatDate - returns "N/A" for empty string', () => {
  assert.strictEqual(formatDate(''), 'N/A');
});

test('formatDate - returns "N/A" for null', () => {
  assert.strictEqual(formatDate(null), 'N/A');
});

test('formatDate - formats valid date string', () => {
  const result = formatDate('2026-08-22');
  assert.strictEqual(typeof result, 'string');
  assert.match(result, /Aug 22, 2026/);
});

test('formatDate - formats ISO datetime', () => {
  const result = formatDate('2026-08-17T10:00:00Z');
  assert.strictEqual(typeof result, 'string');
  assert.match(result, /Aug 17, 2026/);
});

test('escapeHtml - escapes ampersand', () => {
  assert.strictEqual(escapeHtml('Tom & Jerry'), 'Tom &amp; Jerry');
});

test('escapeHtml - escapes less-than', () => {
  assert.strictEqual(escapeHtml('5 < 10'), '5 &lt; 10');
});

test('escapeHtml - escapes greater-than', () => {
  assert.strictEqual(escapeHtml('10 > 5'), '10 &gt; 5');
});

test('escapeHtml - escapes double quotes', () => {
  assert.strictEqual(escapeHtml('Say "hello"'), 'Say &quot;hello&quot;');
});

test('escapeHtml - escapes single quotes', () => {
  assert.strictEqual(escapeHtml("It's fine"), 'It&#039;s fine');
});

test('escapeHtml - escapes HTML tags', () => {
  assert.strictEqual(escapeHtml('<script>alert("xss")</script>'), '&lt;script&gt;alert(&quot;xss&quot;)&lt;/script&gt;');
});

test('escapeHtml - escapes multiple special characters', () => {
  const input = '<div class="test" data-value=\'test\'>';
  const expected = '&lt;div class=&quot;test&quot; data-value=&#039;test&#039;&gt;';
  assert.strictEqual(escapeHtml(input), expected);
});

test('escapeHtml - handles plain text', () => {
  assert.strictEqual(escapeHtml('Hello World'), 'Hello World');
});
