# Review checklist: shared infra (internal packages + cluster-wide k8s manifests)

Reviewed by: QA, Security, Adversarial, Architecture/quality (4/4 complete).
Files in scope: `internal/event/event.go`, `internal/store/store.go`, `internal/streams/streams.go`, `deploy/k8s/{namespace,nats,kustomization,secrets.example}.yaml`, `deploy/k8s/README.md`.

Most items here are duplicates of, or feed into, fixes already listed under the per-service checklists (scraper/web-ui-api both depend on `internal/store`; all three Go services depend on `internal/streams`). Listed here once since the fix lives in shared code.

## To fix (tracked once here — see cross-references)

- [x] **Turso auth token can leak into logs via `store.Connect`'s error wrapping** — `internal/store/store.go:46-61`. Same finding as in `docs/reviews/scraper.md` and `docs/reviews/web-ui-api.md`; fixing it once in the shared package covers both consuming services. See scraper.md for the concrete fix.
- [ ] **telegram-notifier's liveness probe can't run (scratch image has no shell)** — tracked in full in `docs/reviews/telegram-notifier.md`; confirmed independently by this review too (3rd/4th confirmation across all 20 reviews).

## Worth fixing, lower priority

- [x] **Zero test coverage on `internal/event`, `internal/store`, `internal/streams`** — every one of the three Go services depends on these packages, but none of them have `_test.go` files. A field rename on `event.Discovered`, a typo in a stream/subject constant, or a schema drift in `store.Schema` would compile fine and break all three services silently. **Fix (small, worth doing):** add a handful of targeted tests — JSON round-trip tests for the three message structs in `internal/event`, and assertions on the exported constants/config values in `internal/streams` (subject names, `MaxDeliver`, `AckWait`) so a future edit that changes one of these has to consciously update a test, not just the code.

- [x] **`internal/streams`'s `NOTIFICATIONS` stream subscribes via `"notifications.*"` wildcard rather than the two explicit subjects** — `internal/streams/streams.go`. Functionally identical to listing `notifications.sent` and `notifications.failed` explicitly (both are the only subjects ever published on this stream), so this isn't a bug — just looser than the two-subject list in design.md §8. **Fix (optional, cheap):** switch to the explicit two-subject list so a future accidental publish to some other `notifications.*` subject doesn't silently get absorbed into this stream.

- [x] **`hasQuery()` reimplements `strings.Contains`** — `internal/store/store.go:63-69` manually loops over runes to check for `?` instead of `strings.Contains(url, "?")`. Purely a style nit, functionally correct as-is.

- [ ] **`web-ui-frontend` has a readiness probe but no liveness probe; `telegram-notifier` has the reverse gap (once its scratch/shell issue above is fixed, it still only has a liveness probe, no readiness).** Neither is wrong, but a stuck-but-still-"ready" or stuck-but-still-"alive" pod in either direction won't get restarted automatically. Low priority for a personal-scale deployment; worth a look if this ever needs to run genuinely unattended.

## Reviewed — flagged by a reviewer but not real / not actionable as a "fix"

- [ ] **"Missing `BackoffPolicy` on the JetStream consumer config"** (QA-shared-infra finding). Re-read against design.md §6.3, which specifically asks for backoff via `NakWithDelay` (explicit, application-computed per-nak delay) — and that's exactly what `telegram-notifier/handler.go`'s `BackoffDelay()` + `NakWithDelay()` already implement. The consumer-level `BackoffPolicy` field the reviewer wants is a *different*, broker-managed mechanism that would conflict with the app already controlling its own delay. Treating this as a misread of the spec, not a gap — no action.
- [ ] **"No enforcement mechanism for web-ui-api's single-replica requirement"** (flagged by 3 of the 4 reviews for this component set). True — the manifest only has a warning annotation, and `kubectl scale` could still create a second replica that duplicates writes against the same JetStream consumer. Real enforcement (leader election, an admission policy) is a meaningful feature, not a bug fix, and is out of scope for this review pass. Documenting it here as an accepted operational risk rather than queueing it as a "fix" a haiku agent should attempt.
- [ ] **"PII in scraper's `USER_AGENT`" / "CORS wildcard on web-ui-api"** — both already covered as judgment calls (not autonomous fixes) in `docs/reviews/scraper.md`, respectively already-accepted design decisions. Not duplicating the reasoning here.

## Reviewed — no action needed

- [x] Data model DDL (`internal/store/store.go`'s `Schema`) matches design.md §7 exactly; message struct definitions in `internal/event/event.go` match §8 exactly; `EnsureStreams`/`EnsureEventsConsumer` config (`MaxDeliver=5`, `AckWait=30s`, durable name) matches spec; all 6 service manifests are listed in `kustomization.yaml`; NATS service DNS/port (`nats:4222`) is referenced consistently by all three Go services; `secrets.example.yaml` is clearly a CHANGEME template, not real credentials; RBAC is least-privilege (scraper's `Role` has empty rules); container hardening (`runAsNonRoot`, `runAsUser: 1000`, `allowPrivilegeEscalation: false`) is present across every manifest.
