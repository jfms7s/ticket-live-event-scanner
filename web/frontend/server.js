const http = require('http');
const fs = require('fs');
const path = require('path');

const PORT = process.env.PORT || 3000;
const API_BASE_URL = process.env.API_BASE_URL || 'http://localhost:8080';
const PUBLIC_DIR = path.join(__dirname, 'public');

// Generate config.js on startup
function generateConfig() {
  const configContent = `window.API_BASE_URL = ${JSON.stringify(API_BASE_URL)};\n`;
  fs.writeFileSync(path.join(PUBLIC_DIR, 'config.js'), configContent);
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
      '.json': 'application/json; charset=utf-8',
    };

    const contentType = mimeTypes[ext] || 'application/octet-stream';
    res.writeHead(200, { 'Content-Type': contentType });
    res.end(data);
  });
}

const server = http.createServer((req, res) => {
  // Enable CORS for same-cluster requests
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Methods', 'GET, POST, OPTIONS');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type');

  if (req.method === 'OPTIONS') {
    res.writeHead(200);
    res.end();
    return;
  }

  // Route requests
  let filePath = path.join(PUBLIC_DIR, req.url === '/' ? 'index.html' : req.url);

  // Security: prevent directory traversal. Comparing string prefixes alone
  // (`realPath.startsWith(PUBLIC_DIR)`) would wrongly allow a sibling
  // directory like "public-secret" since it shares the "public" prefix;
  // path.relative + a ".." check is the safe way to confirm containment.
  const realPath = path.resolve(filePath);
  const relative = path.relative(PUBLIC_DIR, realPath);
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
