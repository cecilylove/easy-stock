const assert = require('node:assert/strict');
const test = require('node:test');

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { DEFAULT_UPDATE_FEED_URL, resolveUpdateFeedURL, packagedUpdateFeed } = require('../update-feed.cjs');

test('fork updates are disabled by default', () => {
  assert.equal(resolveUpdateFeedURL(''), DEFAULT_UPDATE_FEED_URL);
  assert.equal(DEFAULT_UPDATE_FEED_URL, '');
  assert.equal(resolveUpdateFeedURL(undefined, ''), '');
});

test('packaged fork feed persists without runtime env and upstream source is rejected', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'easy-stock-update-feed-'));
  fs.writeFileSync(path.join(root, 'desktop-update-config.json'), '{"feedURL":"https://updates.example.com/fork"}');
  assert.equal(resolveUpdateFeedURL(undefined, packagedUpdateFeed(root)), 'https://updates.example.com/fork');
  assert.equal(resolveUpdateFeedURL('', packagedUpdateFeed(root)), '');
  assert.throws(() => resolveUpdateFeedURL('https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/updates/desktop'), /own update feed/);
  for (const url of ['https://user:pass@updates.example.com/', 'https://updates.example.com/?token=secret', 'https://updates.example.com/#fragment']) {
    assert.throws(() => resolveUpdateFeedURL(url), /credentials/);
  }
});

test('normalizes and validates a configured HTTPS update feed', () => {
  assert.equal(resolveUpdateFeedURL('https://updates.example.com/desktop///'), 'https://updates.example.com/desktop');
  assert.throws(() => resolveUpdateFeedURL('http://updates.example.com/desktop'), /HTTPS/);
});

test('allows plain HTTP only for loopback feeds used by local end-to-end tests', () => {
  assert.equal(resolveUpdateFeedURL('http://127.0.0.1:8765/updates'), 'http://127.0.0.1:8765/updates');
  assert.equal(resolveUpdateFeedURL('http://localhost:8765/updates///'), 'http://localhost:8765/updates');
  assert.throws(() => resolveUpdateFeedURL('http://192.168.1.10:8765/updates'), /HTTPS/);
});
