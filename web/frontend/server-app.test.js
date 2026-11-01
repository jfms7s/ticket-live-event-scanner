import crypto from 'node:crypto';
import fs from 'node:fs';
import { mkdir, writeFile } from 'node:fs/promises';
import http from 'node:http';
import os from 'node:os';
import path from 'node:path';
import zlib from 'node:zlib';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { buildCsp, createServer, inlineScriptHashes } from './server-app.js';

const INLINE_SCRIPT = "\n    (function () { document.documentElement.dataset.theme = 'dark'; })();\n  ";
const INDEX_HTML = `<!DOCTYPE html><html><head><script>${INLINE_SCRIPT}</script><script src="/config.js"></script>
<script type="module" crossorigin src="/assets/x.js"></script></head><body><div id="root"></div></body></html>`;
const ASSET_JS = `console.log(${JSON.stringify('x'.repeat(2000))});\n`;

let tmp;
let server;
let port;

// request sends a raw request: an explicit path keeps the client from
// normalising "..".
function request(rawPath, { method = 'GET', headers = {} } = {}) {
  return new Promise((resolve, reject) => {
    const req = http.request({ host: '127.0.0.1', port, path: rawPath, method, headers }, (res) => {
      const chunks = [];
      res.on('data', (c) => chunks.push(c));
      res.on('end', () => resolve({ status: res.statusCode, headers: res.headers, body: Buffer.concat(chunks) }));
    });
    req.on('error', reject);
    req.end();
  });
}

beforeAll(async () => {
  tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'server-app-'));
  const dist = path.join(tmp, 'dist');
  await mkdir(path.join(dist, 'assets'), { recursive: true });
  await writeFile(path.join(dist, 'index.html'), INDEX_HTML);
  await writeFile(path.join(dist, 'assets', 'x.js'), ASSET_JS);
  await mkdir(path.join(tmp, 'dist-secret'));
  await writeFile(path.join(tmp, 'dist-secret', 'a'), 'secret');
  await writeFile(path.join(tmp, 'package.json'), '{"secret":true}');

  server = createServer({ distDir: dist, apiBaseUrl: 'https://api.example.test/base' });
  await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
  port = server.address().port;
});

afterAll(async () => {
  await new Promise((resolve) => server.close(resolve));
  fs.rmSync(tmp, { recursive: true, force: true });
});

describe('path traversal', () => {
  it('rejects a raw ../ path with 403', async () => {
    expect((await request('/../package.json')).status).toBe(403);
  });

  it('does not serve an encoded ../ path', async () => {
    const res = await request('/%2e%2e/package.json');
    expect(res.status).not.toBe(200);
    expect(res.body.toString()).not.toContain('secret');
  });

  it('rejects a sibling directory sharing the dist prefix with 403', async () => {
    expect((await request('/../dist-secret/a')).status).toBe(403);
  });

  it('returns 404 for a missing file', async () => {
    expect((await request('/nope.js')).status).toBe(404);
  });
});

describe('caching', () => {
  it('serves hashed assets as immutable', async () => {
    const res = await request('/assets/x.js');
    expect(res.status).toBe(200);
    expect(res.headers['cache-control']).toBe('public, max-age=31536000, immutable');
    expect(res.headers['content-type']).toBe('application/javascript; charset=utf-8');
  });

  it('revalidates index.html at / and /index.html', async () => {
    for (const p of ['/', '/index.html']) {
      const res = await request(p);
      expect(res.status).toBe(200);
      expect(res.headers['cache-control']).toBe('no-cache');
      expect(res.body.toString()).toBe(INDEX_HTML);
    }
  });
});

describe('gzip', () => {
  it('compresses when the client accepts gzip', async () => {
    const res = await request('/assets/x.js', { headers: { 'Accept-Encoding': 'br, gzip' } });
    expect(res.headers['content-encoding']).toBe('gzip');
    expect(res.headers['vary']).toBe('Accept-Encoding');
    expect(res.body.length).toBeLessThan(ASSET_JS.length);
    expect(zlib.gunzipSync(res.body).toString()).toBe(ASSET_JS);
  });

  it('sends identity without Accept-Encoding or with gzip;q=0', async () => {
    for (const headers of [{}, { 'Accept-Encoding': 'gzip;q=0' }]) {
      const res = await request('/assets/x.js', { headers });
      expect(res.headers['content-encoding']).toBeUndefined();
      expect(res.headers['vary']).toBe('Accept-Encoding');
      expect(res.body.toString()).toBe(ASSET_JS);
    }
  });
});

describe('config.js', () => {
  it('is served from memory with the API base URL', async () => {
    const res = await request('/config.js');
    expect(res.status).toBe(200);
    expect(res.body.toString()).toBe('window.API_BASE_URL = "https://api.example.test/base";\n');
    expect(res.headers['cache-control']).toBe('no-cache');
    expect(fs.existsSync(path.join(tmp, 'dist', 'config.js'))).toBe(false);
  });
});

describe('security headers', () => {
  it('are on every response, errors included', async () => {
    for (const p of ['/', '/assets/x.js', '/config.js', '/nope.js', '/../package.json']) {
      const res = await request(p);
      expect(res.headers['x-content-type-options'], p).toBe('nosniff');
      expect(res.headers['referrer-policy'], p).toBe('strict-origin-when-cross-origin');
      expect(res.headers['content-security-policy'], p).toBeTruthy();
    }
  });

  it('allow the inline script by hash and the API origin', async () => {
    const csp = (await request('/')).headers['content-security-policy'];
    const hash = crypto.createHash('sha256').update(INLINE_SCRIPT).digest('base64');
    expect(csp).toContain(`script-src 'self' 'sha256-${hash}'`);
    expect(csp).not.toContain('unsafe-inline');
    expect(csp).toContain("connect-src 'self' https://api.example.test;");
    expect(csp).toContain("frame-ancestors 'none'");
    expect(csp).toContain("style-src 'self'");
  });

  it('rejects other methods with 405', async () => {
    const res = await request('/', { method: 'POST' });
    expect(res.status).toBe(405);
    expect(res.headers['allow']).toBe('GET, HEAD');
  });
});

describe('inlineScriptHashes', () => {
  it('hashes only scripts without src', () => {
    expect(inlineScriptHashes('<script src="/a.js"></script><script type="module" src="/b.js"></script>')).toEqual([]);
    expect(inlineScriptHashes('<script>a</script><SCRIPT>b</SCRIPT>')).toHaveLength(2);
  });
});

describe('buildCsp', () => {
  it('keeps connect-src same-origin for a relative API base URL', () => {
    expect(buildCsp({ scriptHashes: [], apiBaseUrl: '/api' })).toContain("connect-src 'self';");
  });
});
