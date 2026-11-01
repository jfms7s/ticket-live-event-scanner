# ticket-live-event-scanner

Watches ticketline.pt for new events and posts each one to a Telegram chat, tracks whether the notification was delivered, and serves a small web dashboard with a manual retry. It is a set of Go services (`cmd/scraper`, `cmd/telegram-notifier`, `cmd/email-notifier`, `cmd/web-ui-api`) talking over NATS JetStream and storing state in SQLite/Turso, plus a React frontend in `web/frontend`. Images are built and pushed to GHCR when a `v*` tag is pushed (or the workflow is run by hand), and only after the checks below pass.

## Local checks

These are the commands `.github/workflows/test.yml` runs, from the repo root.

Go (`go.mod` asks for Go 1.25; `-race` needs cgo):

```bash
test -z "$(gofmt -l .)"
go vet ./...
go test -count=1 -race ./...
```

Frontend (Node 24, the same major as `web/frontend/Dockerfile`; Vite 8 needs Node >= 20.19):

```bash
cd web/frontend
npm ci
npm test
npm run build
```
