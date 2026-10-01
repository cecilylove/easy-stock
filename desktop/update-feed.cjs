const fs = require('node:fs');
const path = require('node:path');
const DEFAULT_UPDATE_FEED_URL = '';
const UPSTREAM_UPDATE_HOST = 'easy-stock-fs.oss-cn-beijing.aliyuncs.com';

const LOOPBACK_HOSTNAMES = new Set(['localhost', '127.0.0.1', '[::1]', '::1']);

function resolveUpdateFeedURL(configuredURL = process.env.A_STOCK_UPDATE_FEED_URL, packagedURL = '') {
  const value = String(configuredURL ?? packagedURL).trim().replace(/\/+$/, '');
  if (!value) return '';
  const parsed = new URL(value);
  const isLoopback = LOOPBACK_HOSTNAMES.has(parsed.hostname);
  if (parsed.hostname === UPSTREAM_UPDATE_HOST) throw new Error('Fork builds must configure their own update feed');
  if (parsed.username || parsed.password || parsed.search || parsed.hash) throw new Error('Desktop update feed must not contain credentials, query parameters or fragments');
  if (parsed.protocol !== 'https:' && !(parsed.protocol === 'http:' && isLoopback)) {
    throw new Error('Desktop update feed must use HTTPS');
  }
  return parsed.toString().replace(/\/$/, '');
}

function packagedUpdateFeed(resourcesRoot) {
  const file = path.join(resourcesRoot, 'desktop-update-config.json');
  if (!fs.existsSync(file)) return '';
  const config = JSON.parse(fs.readFileSync(file, 'utf8'));
  return resolveUpdateFeedURL(config.feedURL || '');
}

module.exports = { DEFAULT_UPDATE_FEED_URL, resolveUpdateFeedURL, packagedUpdateFeed };
