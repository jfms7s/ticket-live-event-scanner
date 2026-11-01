import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createServer } from './server-app.js';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

const PORT = process.env.PORT || 3000;
// Served as /config.js at runtime (not baked in at build time) so the same
// image can be deployed against different API_BASE_URL values.
const API_BASE_URL = process.env.API_BASE_URL || 'http://localhost:8080';

const server = createServer({ distDir: path.join(__dirname, 'dist'), apiBaseUrl: API_BASE_URL });

server.listen(PORT, () => {
  console.log(`Web server listening on http://localhost:${PORT}`);
  console.log(`API_BASE_URL: ${API_BASE_URL}`);
});

// As PID 1 in the container Node ignores SIGTERM by default, so the pod
// waited out its grace period on every rollout. Nothing to flush: exit.
for (const signal of ['SIGTERM', 'SIGINT']) {
  process.on(signal, () => process.exit(0));
}
