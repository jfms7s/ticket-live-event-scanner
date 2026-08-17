# Review checklist: scraper

Reviewed by: QA, Security, Adversarial, Architecture/quality (4/4 complete).
Files in scope: `cmd/scraper/main.go`, `cmd/scraper/parser.go`, `cmd/scraper/parser_test.go`, `cmd/scraper/Dockerfile`, `deploy/k8s/scraper.yaml`.

Every item below was cross-checked against the actual current source (not just the reviewer transcripts) before being added here.

## To fix

- [x] **Error aggregation silently masks failures in `discover()`** — `cmd/scraper/main.go:192-291`. `lastErr` only keeps the *last* error seen across the whole run; `fetchHubPage()` (line 310-337) logs its per-session fetch errors internally but never returns them, so a failure there never reaches `lastErr` at all. Net effect: a run with several failed fetches can still exit 0, and the CronJob/operator never finds out. **Fix:** track a failure count (not just the last error) across both the hub-page loop and the month-walk loop; have `fetchHubPage` return an aggregated error (or accept an error-counter callback) instead of only logging; increment `stats.Errors` once per actual failure, not once per run. Flagged independently by all 3 non-security reviewers.

- [ ] **Turso auth token can leak into logs via connection errors** — `internal/store/store.go:46-61` (shared package, also affects web-ui-api). `Connect()` builds the DSN with the token embedded in the URL (`...?authToken=<secret>`, line 53) and returns `sql.Open`'s error wrapped with `%w` (line 58). If the libsql driver ever includes the DSN in its error text, the token ends up in `log.Fatalf` output in both `cmd/scraper/main.go` and `cmd/web-ui-api/main.go`. **Fix:** in `store.Connect`, don't propagate the raw driver error; return a fixed, generic error (e.g. `"open turso connection: connection failed (check TURSO_DATABASE_URL/TURSO_AUTH_TOKEN)"`) instead of `%w`-wrapping it. One fix in the shared package covers both call sites. Flagged independently by the scraper-security and web-ui-api-security reviews.

- [x] **LRU cache grows unboundedly / can produce a false cache-miss** — `cmd/scraper/main.go:465-482` (`cacheURLResponse`). Every call unconditionally appends `url` to `cacheOrder` (line 481), even when the URL is already a key in `urlCache` (a re-fetch after a 200 revalidation, or after prior eviction-and-refetch). Confirmed by tracing it: this creates duplicate entries in `cacheOrder`, which (a) lets the slice grow without bound over a long-running process and (b) can cause the *wrong* entry to be evicted from `urlCache`, so a later 304 response finds no cached body and returns an error instead of the cached content. **Fix:** in `cacheURLResponse`, check whether `url` is already a key in `urlCache` before appending to `cacheOrder`; only append on a genuinely new key.

- [x] **Dockerfile UID not explicit** — `cmd/scraper/Dockerfile` (`adduser -D scraper`, no `-u 1000`) relies on the image happening to assign UID 1000 to the first created user, while `deploy/k8s/scraper.yaml` hardcodes `runAsUser: 1000`. Works today but is a fragile implicit coupling. **Fix:** `adduser -D -u 1000 scraper` (or equivalent explicit UID/GID) so the Dockerfile and manifest agree by construction, not by accident.

## Worth fixing, lower priority

- [x] **No format validation on scraped `event_date`** — `cmd/scraper/parser.go`. `startDate`/`data-date` values are stored verbatim; a malformed date from the HTML (or a future markup change) would flow through to NATS unchanged, violating the `YYYY-MM-DD` contract in design.md §8. **Fix:** validate with `time.Parse("2006-01-02", ...)` before publishing; log and skip (or flag) events that fail.

- [x] **Month-arithmetic overflow if `AgendaMonthsAhead` ≥ 13** — `cmd/scraper/main.go:227-235` only subtracts 12 once, so `currentMonth + i` above 24 produces an invalid month number. Not reachable with the current default (`AgendaMonthsAhead=2`), so this is latent, not active. **Fix:** normalize with `((currentMonth - 1 + i) % 12) + 1` and increment year via integer division, so it's correct regardless of config.

## Needs a decision, not a blind fix

- [ ] **Scraper never fetches `/agenda/{year}/{month}` directly — only `/pesquisa/?month=&year=&page=`.** Checked `docs/design.md` (§4, line 45-46): *"Discovery entry point: `/agenda/{year}/{month}` lists events for that month, paginated via `/pesquisa/?month={m}&year={y}&page={n}`."* Read literally, `/agenda/{year}/{month}` is page 1 and `/pesquisa` only covers page ≥2 — but `fetchSearchPage()` (`cmd/scraper/main.go:339-349`) uses `/pesquisa/?...&page=1` for the first page too, and no code path ever requests a literal `/agenda/...` URL. This may be a real gap (missing page-1 content if the two endpoints differ) or a functional non-issue (if ticketline.pt's `/pesquisa?page=1` returns the same listing as `/agenda/{y}/{m}` — plausible, since design.md's own research already treated them as interchangeable pagination steps of one listing). I can't tell which without a live request against the real site, which I didn't make. **Recommend:** either confirm empirically (one polite GET to both URLs for the current month and diff the event cards) or explicitly amend design.md to state `/pesquisa` is the sole entry point if that's the intended reading — don't have a fixer agent guess at this one.

- [ ] **`USER_AGENT` includes a personal email address** — `deploy/k8s/scraper.yaml` (contact `jfms7s@gmail.com`). Flagged as PII exposure by the shared-infra security review, but this looks like it matches the *already-confirmed* "honest User-Agent + contact info" politeness decision in design.md §4 (line 79: *"identify with a descriptive... User-Agent + contact info"*), which commonly means a real, reachable email. Not treating this as a bug to silently fix — flagging so you can confirm the email is meant to be public before anyone changes or keeps it.

## Reviewed — no action needed

- [x] Message schema, NATS publishing/dedup, User-Agent string, request delay, ETag/If-Modified-Since caching, URL-scope validation, RBAC, non-root container, HTML-size limiting, hub-page discovery logic — all confirmed correct against design.md by at least one reviewer, no changes needed.
- [x] SQL injection, SSRF, deserialization safety — confirmed safe by the security review (parameterized queries only, allowlisted outbound URLs, size-capped HTML parsing).
