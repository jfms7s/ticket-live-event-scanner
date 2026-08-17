# Review of the fixer pass — 2026-08-17

After the five haiku fixer agents applied docs/reviews/{scraper,telegram-notifier,web-ui-api,web-ui-frontend,shared-infra}.md, the same 4 reviewer roles (QA, security, adversarial, architecture/quality) were run again against the resulting diff. `go build ./...`, `go vet ./...`, `go test ./...` (75 tests) and the frontend's `npm test` (16 tests) all pass. Architecture/quality found no issues — cross-component integration (message schemas, consumer config constants, subject names) held up correctly across the five concurrently-edited slices.

Three real issues survived my own verification against the source (one reviewer claim was checked and turned out to be inaccurate — see below). None are deployment blockers; all are worth a follow-up pass.

## To fix

- [x] **web-ui-frontend Dockerfile still defaults to root outside Kubernetes** — `web/frontend/Dockerfile`. Fixed: added `USER 1000:1000` right after the `chown` line, so the image is non-root by construction, not just under the K8s manifest's `securityContext`.

- [x] **Scraper's error count is bumped twice on a discovery failure** — `cmd/scraper/main.go:118`. Fixed: removed the vestigial `scraper.stats.Errors++` in `run()`'s error branch; `discover()` is now the sole place that sets `stats.Errors`.

- [x] **Reason-based retrigger idempotency fix has no test proving the fixed behavior** — `cmd/web-ui-api/database.go:48-63`. Fixed: added `TestManualRetryWithExistingPendingNotification` (asserts a manual retrigger inserts a new pending row even when one already exists) and `TestDiscoveryIdempotencyWithExistingPending` (asserts the discovery/redelivery path still skips in the same situation) to `cmd/web-ui-api/main_test.go`.

## Worth fixing, lower priority

- [x] **No tests for four other fixer-pass behavior changes** — all four added:
  - `cmd/scraper/parser.go` `validateEventDate()` — `TestValidateEventDateValid`/`Empty`/`Malformed` added to `parser_test.go`.
  - `cmd/scraper/main.go` `cacheURLResponse` LRU fix — `TestCacheURLResponseLRUNoDuplicate`/`TestCacheURLResponseLRUUpdateOrder` added to the new `cmd/scraper/main_test.go`.
  - `cmd/scraper/main.go` month-arithmetic normalization — the fixer's first pass duplicated the formula as a literal inside the test instead of calling production code (a tautological test that couldn't catch a regression in `discover()` itself); fixed directly by extracting the arithmetic into a pure `normalizeMonth(currentMonth, currentYear, offset int) (month, year int)` helper that `discover()` now calls, and pointing `TestMonthNormalizationSimple` at that helper instead of a copy of the formula.
  - `cmd/scraper/main.go` error aggregation (`errorCount`) — `TestFetchHubPageErrorAggregation` was added but is documentation-only (a `t.Log`, no assertion, doesn't exercise `fetchHubPage`); left as-is since `fetchHubPage` requires HTTP mocking to test meaningfully and this was flagged upfront as an acceptable outcome for this item. Genuine coverage here would need an HTTP-mockable refactor, which is out of scope for this pass.

## Reviewed — no action needed

- [x] Concurrency/shutdown changes (web-ui-api's context-propagated consumer goroutines + `msgsChan.Stop()` watchers; telegram-notifier's `ConsumeContext.Stop()` and `Nats-Num-Delivered` header fallback) — adversarially reviewed, no race or shutdown-ordering bug found.
- [x] `Nats-Msg-Id` construction in telegram-notifier (`sent-{eventID}-{natsSeq}` / `failed-...`) — confirmed unique and stable across the redelivery scenarios it's meant to dedup.
- [x] Turso auth-token leak fix in `internal/store/store.go` — confirmed the generic error message fully replaces the raw driver error, no other route (log call, `%v`, caller) still leaks the token.
- [x] telegram-notifier's `scratch`→`alpine` Dockerfile switch — no extra attack surface beyond the intended `ca-certificates` + non-root user.
- [x] New tests under `internal/event`, `internal/store`, `internal/streams` — meaningful (schema/round-trip/constant assertions), no hardcoded-looking credentials in fixtures.
- [x] Cross-component integration between the five concurrently-edited slices (message schemas, `streams.EventsConsumerMaxDeliver`/`AckWait` constants, subject names) — held up correctly; each fixer agent's isolated edits compose without drift.
