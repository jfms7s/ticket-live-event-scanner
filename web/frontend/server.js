import http from 'http';
import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const PORT = process.env.PORT || 3000;
const API_BASE_URL = process.env.API_BASE_URL || 'http://localhost:8080';
const DIST_DIR = path.join(__dirname, 'dist');

// Generate config.js on startup. Written into the build output (rather than
// baked in at build time) so the same image can be deployed against
// different API_BASE_URL values without rebuilding.
function generateConfig() {
  const configContent = `window.API_BASE_URL = ${JSON.stringify(API_BASE_URL)};\n`;
  try {
    fs.writeFileSync(path.join(DIST_DIR, 'config.js'), configContent);
  } catch (error) {
    console.error('Failed to write config.js:', error);
    process.exit(1);
  }
}

// Serve static files
function serveFile(filePath, res) {
  fs.readFile(filePath, (err, data) => {
    if (err) {
      res.writeHead(404, { 'Content-Type': 'text/plain' });
      res.end('404 Not Found');
      return;
    }

    const ext = path.extname(filePath);
    const mimeTypes = {
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

    const contentType = mimeTypes[ext] || 'application/octet-stream';
    res.writeHead(200, { 'Content-Type': contentType });
    res.end(data);
  });
}

const server = http.createServer((req, res) => {
  const requestPath = req.url === '/' ? 'index.html' : req.url.split('?')[0];
  let filePath = path.join(DIST_DIR, requestPath);

  // Security: prevent directory traversal. Comparing string prefixes alone
  // (`realPath.startsWith(DIST_DIR)`) would wrongly allow a sibling
  // directory like "dist-secret" since it shares the "dist" prefix;
  // path.relative + a ".." check is the safe way to confirm containment.
  const realPath = path.resolve(filePath);
  const relative = path.relative(DIST_DIR, realPath);
  if (relative.startsWith('..') || path.isAbsolute(relative)) {
    res.writeHead(403, { 'Content-Type': 'text/plain' });
    res.end('403 Forbidden');
    return;
  }

  serveFile(realPath, res);
});

server.listen(PORT, () => {
  generateConfig();
  console.log(`Web server listening on http://localhost:${PORT}`);
  console.log(`API_BASE_URL: ${API_BASE_URL}`);
});
