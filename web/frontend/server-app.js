// Static file server for the built dashboard. createServer has no side
// effects at import so tests can run it against a temporary dist directory;
// server.js reads the environment and listens.
import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import path from 'node:path';
import zlib from 'node:zlib';

const MIME_TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.js': 'application/javascript; charset=utf-8',
  '.mjs': 'application/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.ico': 'image/x-icon',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
};

// Text types worth gzipping; images and fonts are already compressed.
const COMPRESSIBLE = new Set(['.html', '.css', '.js', '.mjs', '.json', '.svg']);

// Vite content-hashes everything under /assets/, so a changed file gets a
// new URL and these can be cached forever. Everything else (index.html,
// config.js) must be revalidated so a new build is picked up.
const IMMUTABLE = 'public, max-age=31536000, immutable';
const REVALIDATE = 'no-cache';

/**
 * Returns the CSP hash source ('sha256-…') of every inline <script> (one
 * without a src attribute) in html, so the theme bootstrap in index.html
 * can run under a CSP without 'unsafe-inline'.
 */
export function inlineScriptHashes(html) {
  const hashes = [];
  const re = /<script\b([^>]*)>([\s\S]*?)<\/script>/gi;
  for (const [, attrs, body] of html.matchAll(re)) {
    if (/\bsrc\s*=/i.test(attrs)) continue;
    const digest = crypto.createHash('sha256').update(body, 'utf8').digest('base64');
    hashes.push(`'sha256-${digest}'`);
  }
  return hashes;
}

/** The origin of apiBaseUrl when it is an absolute http(s) URL, else null. */
function apiOrigin(apiBaseUrl) {
  try {
    const url = new URL(apiBaseUrl);
    return url.protocol === 'http:' || url.protocol === 'https:' ? url.origin : null;
  } catch {
    return null; // relative, e.g. "/api": same origin
  }
}

export function buildCsp({ scriptHashes, apiBaseUrl }) {
  const connect = ["'self'"];
  const origin = apiOrigin(apiBaseUrl);
  if (origin) connect.push(origin);
  return [
    "default-src 'self'",
    `script-src 'self' ${scriptHashes.join(' ')}`.trim(),
    "img-src 'self' https: data:",
    `connect-src ${connect.join(' ')}`,
    "style-src 'self'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join('; ');
}

function acceptsGzip(req) {
  const header = req.headers['accept-encoding'] || '';
  return header
    .split(',')
    .map((part) => part.trim().split(';'))
    .some(([coding, ...params]) => coding.trim() === 'gzip' && !params.some((p) => /^\s*q=0(\.0*)?\s*$/.test(p)));
}

/**
 * Creates the HTTP server (not listening). distDir is the Vite build
 * output; apiBaseUrl is served as window.API_BASE_URL from /config.js,
 * built here rather than written into distDir so the image needs no
 * writable files.
 */
export function createServer({ distDir, apiBaseUrl }) {
  const root = path.resolve(distDir);

  let indexHtml = '';
  try {
    indexHtml = fs.readFileSync(path.join(root, 'index.html'), 'utf8');
  } catch (err) {
    console.error(`Failed to read ${path.join(root, 'index.html')}:`, err.message);
  }

  const securityHeaders = {
    'X-Content-Type-Options': 'nosniff',
    'Referrer-Policy': 'strict-origin-when-cross-origin',
    'Content-Security-Policy': buildCsp({ scriptHashes: inlineScriptHashes(indexHtml), apiBaseUrl }),
  };

  const configJs = Buffer.from(`window.API_BASE_URL = ${JSON.stringify(apiBaseUrl)};\n`);

  // Build output never changes at runtime, so each file is read (and
  // gzipped) once. Only files that exist are cached.
  const cache = new Map();

  function load(filePath) {
    let entry = cache.get(filePath);
    if (!entry) {
      entry = { data: fs.readFileSync(filePath), gzip: null };
      cache.set(filePath, entry);
    }
    return entry;
  }

  function send(req, res, status, headers, body) {
    res.writeHead(status, { ...securityHeaders, ...headers, 'Content-Length': body.length });
    res.end(req.method === 'HEAD' ? undefined : body);
  }

  function sendText(req, res, status, text) {
    send(req, res, status, { 'Content-Type': 'text/plain; charset=utf-8' }, Buffer.from(text));
  }

  function serveFile(req, res, filePath, requestPath) {
    let entry;
    try {
      entry = load(filePath);
    } catch {
      sendText(req, res, 404, '404 Not Found');
      return;
    }

    const ext = path.extname(filePath).toLowerCase();
    const headers = {
      'Content-Type': MIME_TYPES[ext] || 'application/octet-stream',
      'Cache-Control': requestPath.startsWith('/assets/') ? IMMUTABLE : REVALIDATE,
    };

    let body = entry.data;
    if (COMPRESSIBLE.has(ext)) {
      headers['Vary'] = 'Accept-Encoding';
      if (acceptsGzip(req)) {
        entry.gzip ??= zlib.gzipSync(entry.data);
        body = entry.gzip;
        headers['Content-Encoding'] = 'gzip';
      }
    }
    send(req, res, 200, headers, body);
  }

  return http.createServer((req, res) => {
    if (req.method !== 'GET' && req.method !== 'HEAD') {
      res.setHeader('Allow', 'GET, HEAD');
      sendText(req, res, 405, '405 Method Not Allowed');
      return;
    }

    const urlPath = (req.url || '/').split('?')[0];
    const requestPath = urlPath === '/' ? '/index.html' : urlPath;

    if (requestPath === '/config.js') {
      send(req, res, 200, { 'Content-Type': MIME_TYPES['.js'], 'Cache-Control': REVALIDATE }, configJs);
      return;
    }

    // Security: prevent directory traversal. Comparing string prefixes alone
    // (`realPath.startsWith(root)`) would wrongly allow a sibling directory
    // like "dist-secret" since it shares the "dist" prefix; path.relative +
    // a ".." check is the safe way to confirm containment.
    const realPath = path.resolve(path.join(root, requestPath));
    const relative = path.relative(root, realPath);
    if (relative.startsWith('..') || path.isAbsolute(relative)) {
      sendText(req, res, 403, '403 Forbidden');
      return;
    }

    serveFile(req, res, realPath, requestPath);
  });
}
