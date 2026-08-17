# Review pass — 2026-08-17

20 reviews (4 roles × 5 components — QA, security, adversarial, architecture/quality, each run against scraper, telegram-notifier, web-ui-api, web-ui-frontend, and shared infra) were run in parallel and consolidated into a per-component checklist. Every item below was cross-checked against the current source by hand before being listed — several reviewer claims turned out to be false positives (a misread of the NATS client API, a misread of the backoff spec) and were dropped or downgraded rather than queued as fixes; those are called out explicitly in each file's "Reviewed — no action" / "finding rejected" sections so the reasoning isn't lost.

- [scraper.md](scraper.md) — 4 fixes, 2 lower-priority, 2 items needing a decision rather than a blind fix (does discovery need to hit `/agenda` directly? is the User-Agent's personal email intentional?)
- [telegram-notifier.md](telegram-notifier.md) — 3 fixes (one is a real deployment blocker: liveness probe can't run in a `scratch` container), 3 lower-priority, 1 accepted limitation, 1 rejected finding
- [web-ui-api.md](web-ui-api.md) — 3 fixes (retrigger idempotency, missing consumer delivery limits, shutdown context), 4 lower-priority
- [web-ui-frontend.md](web-ui-frontend.md) — 3 fixes (one is a real deployment blocker: the container can't write its own config file as the non-root user the manifest requires), 3 lower-priority
- [shared-infra.md](shared-infra.md) — mostly cross-references into the above (shared code = one fix covers multiple services), plus test-coverage and probe gaps

All "To fix" and "Worth fixing, lower priority" items across the five checklists above have since been implemented (five parallel haiku fixer agents, one per component). The fixes were re-reviewed by the same 4 roles; that follow-up pass turned up 3 real issues (one Dockerfile gap, one dead-code cleanup, one missing regression test) plus 4 lower-priority test-coverage gaps — see [fixer-pass-review.md](fixer-pass-review.md).

## Two deployment blockers worth knowing about before anything else

Both would only surface once actually deployed to a real cluster, which is presumably why review alone caught them and testing didn't:

1. **telegram-notifier**: exec liveness probe (`kill -0 1` via `sh`) against a `FROM scratch` image with no shell — probe always fails, pod restart-loops.
2. **web-ui-frontend**: writes `config.js` into a root-owned directory at runtime while the manifest forces `runAsUser: 1000` — `EACCES` on startup.

Next step, once you're ready: dispatch fixer agents against these checklists (one per component, looping against the same 4 reviewer angles until clean), or fix the two deployment blockers directly given how cheap and unambiguous they are.
