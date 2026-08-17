# Review checklist: web-ui-frontend

Reviewed by: QA, Security, Adversarial, Architecture/quality (4/4 complete).
Files in scope: `web/frontend/server.js`, `web/frontend/public/{app.js,index.html,style.css,utils.js}`, `web/frontend/utils.js`, `web/frontend/test/utils.test.js`, `web/frontend/package.json`, `web/frontend/Dockerfile`, `deploy/k8s/web-ui-frontend.yaml`.

I read the current `Dockerfile` and `index.html` directly to check the "container won't start in Kubernetes" claim below — it's real and is the most important item in this file.

## To fix

- [x] **Container will fail to start under the Kubernetes manifest's `securityContext`** — `web/frontend/server.js` writes `public/config.js` at runtime (`generateConfig()`), but `web/frontend/Dockerfile` does a plain `COPY public/ public/` with no ownership change, so the directory is root-owned in the built image. `deploy/k8s/web-ui-frontend.yaml` enforces `runAsNonRoot: true` / `runAsUser: 1000`. Non-root UID 1000 cannot write into a root-owned directory — the pod will crash on startup with `EACCES` the moment it's actually deployed to the cluster with that manifest. This is a deployment blocker, confirmed independently by the QA and adversarial reviews and verified by reading the Dockerfile myself. **Fix:** add `RUN mkdir -p /app/public && chown -R 1000:1000 /app` to the Dockerfile after the `COPY` steps, so UID 1000 owns the directory it needs to write into at startup.

- [x] **"None yet" notification filter is unreachable in the UI** — `web/frontend/public/app.js` has working filter logic for events with no notification (`notificationStatusFilter === 'none'`) and the CSS badge for it exists, but `web/frontend/public/index.html:32-37`'s `<select id="notificationStatusFilter">` never offers a `"none"` `<option>` — confirmed by reading `index.html` directly. Users can never actually select this filter even though everything downstream supports it. **Fix:** add `<option value="none">None yet</option>` to the select.

- [x] **Retrigger button can be double-clicked into firing twice** — `web/frontend/public/app.js` (`handleRetrigger`) re-enables the button as soon as the fetch resolves, but the "Retriggered" status message stays visible for 3 more seconds afterward. A user clicking again inside that 3-second window fires a second retrigger request for the same event. **Fix:** keep the button disabled until the status message actually clears (move `btn.disabled = false` into the same `setTimeout` that clears the message), or track an in-flight flag per event ID.

## Worth fixing, lower priority

- [x] **`window.API_BASE_URL` is used without checking it's defined** — `web/frontend/public/app.js` builds `` `${window.API_BASE_URL}/api/events` `` unconditionally. If `config.js` ever fails to load (the permission bug above being the most likely cause, but also just a network hiccup), this silently becomes a fetch to the literal string `"undefined/api/events"` with no diagnostic beyond a generic "failed to fetch" banner. **Fix:** check `if (!window.API_BASE_URL)` up front and show an explicit configuration-error message instead of trying to fetch. Becomes lower-priority once the Dockerfile permission fix above lands, since that removes the realistic way `config.js` fails to generate — but worth keeping as a guard regardless.

- [x] **`fs.writeFileSync` in `generateConfig()` has no error handling** — `web/frontend/server.js`. If it throws (permissions, disk full), there's no explicit catch/log before the process would crash on an unhandled exception at startup. Now that the Dockerfile fix removes the main way this fails, this becomes a defensive nicety rather than a live bug — a clear `try { ... } catch (e) { console.error(...); process.exit(1); }` around the call still makes the failure mode obvious instead of an unhandled-exception stack trace.

- [x] **Unnecessary CORS headers on the static file server** — `web/frontend/server.js` sets `Access-Control-Allow-Origin: *` on every static asset response. Static HTML/CSS/JS served same-origin don't need CORS headers at all (only the API's cross-origin fetches do, which `web-ui-api` already handles). Harmless but confusing. **Fix:** remove the CORS header block from `server.js`.

## Reviewed — leaving as-is (not worth the churn)

- [ ] **`web/frontend/utils.js` and `web/frontend/public/utils.js` are byte-identical duplicates.** One is imported by the Node test suite (CommonJS), the other loaded by the browser via `<script>`. A shared-module build step would remove the duplication but is disproportionate tooling for a project this size — leaving both files as-is; just keep them in sync if either changes.
- [ ] **`escapeHtml()` is exported and tested but never called** — `app.js` already uses `.textContent` everywhere for API-derived data (confirmed safe by the security review), so the helper is redundant, not a vulnerability. Harmless dead code; not worth removing given it's covered by an existing test and may be convenient if a future feature needs raw HTML insertion.

## Reviewed — no action needed

- [x] Path-traversal fix in `server.js` (verified correct: uses `path.relative` + `..`/absolute-path rejection, not the earlier `startsWith` anti-pattern), all DOM insertion via safe `textContent`/`createElement` (no `innerHTML` of untrusted data), no hardcoded secrets, REST API paths/methods/response-shape match `web-ui-api` exactly, non-root k8s `securityContext`, frontend/backend deployed as separate pods per design.md §9.
