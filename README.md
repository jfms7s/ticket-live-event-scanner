# ticket-live-event-scanner

Watches ticketline.pt for new events and posts each one to a Telegram chat, tracks whether the notification was delivered, and serves a small web dashboard with a manual retry. It is one Go service, `cmd/web-ui-api`, talking to NATS JetStream: a long-running process that scrapes ticketline.pt every hour and sends the weekly digest on Sunday evening, consumes the events, delivers the Telegram messages and calendar-invite emails (a channel without its credentials is disabled, not fatal), and serves the API and the dashboard and storing state in SQLite/Turso, plus a React frontend in `web/frontend`, built into the `web-ui-api` binary (`go:embed`) and served by it at `/`, same origin as `/api`. Images are built and pushed to GHCR when a `v*` tag is pushed (or the workflow is run by hand), and only after the checks below pass.

## Local checks

These are the commands `.github/workflows/test.yml` runs, from the repo root.

Go (`go.mod` asks for Go 1.25; `-race` needs cgo):

```bash
test -z "$(gofmt -l .)"
go vet ./...
go test -count=1 -race ./...
```

Frontend (Node 24, the same major as `cmd/web-ui-api/Dockerfile`; Vite 8 needs Node >= 20.19):

```bash
cd web/frontend
npm ci
npm test
npm run build
```
