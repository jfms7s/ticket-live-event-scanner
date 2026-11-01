# Kubernetes Manifests for Ticket Scanner

> **Manual/dev manifests.** Production runs the `ticket-live-event-scanner-helm`
> chart from the `helm-charts` repo, deployed by Argo CD. These files are not kept
> in step with it.

This directory contains Kubernetes manifests to deploy the ticket-scanner services in the `ticket-scanner` namespace.

## Components

- **namespace.yaml**: Creates the `ticket-scanner` namespace
- **nats.yaml**: NATS JetStream server (StatefulSet + Service)
  - StatefulSet with 1 replica, image `nats:2-alpine`
  - PersistentVolumeClaim 5Gi for JetStream file storage
  - Headless ClusterIP service on port 4222
- **scraper.yaml**: Ticket discovery scraper (CronJob)
  - Scheduled hourly (`0 * * * *`)
  - Pulls event data from ticketline.pt, publishes to NATS `events.discovered`
- **telegram-notifier.yaml**: Telegram notification service (Deployment)
  - 1 replica consuming `events.discovered` from NATS
  - Sends Telegram messages, publishes `notifications.sent/failed` results
- **email-notifier.yaml**: Calendar-invite email service (Deployment)
  - 1 replica consuming `events.purchased` from NATS
  - Sends an email with an `.ics` invite over SMTP, publishes `notifications.email.sent/failed` results
- **web-ui-api.yaml**: Web API backend (Deployment + Service)
  - Go JSON API on port 8080 (exposed via Service on port 80)
  - Liveness/readiness probes on `GET /healthz`
  - Consumes both NATS streams (`EVENTS`, `NOTIFICATIONS`), materializes into the libsql database
- **web-ui-frontend.yaml**: Web UI frontend (Deployment + Service)
  - JavaScript/frontend server on port 3000 (exposed via Service on port 80)
  - Calls `web-ui-api` internally
  - These manifests define no Ingress
- **secrets.example.yaml**: Placeholder Secret templates (see "Secrets" section below)
- **kustomization.yaml**: Kustomize index, includes all resources

## Images

The raw manifests reference local placeholder image names (e.g. `ticket-scanner/scraper:local`).
`kustomization.yaml` rewrites these at apply-time via its `images:` section, so
`kubectl apply -k deploy/k8s` always deploys the GHCR images, not the placeholders.

### GitHub Container Registry (default)

`.github/workflows/build-push.yml` builds and pushes all five images to GHCR (multi-arch,
amd64 + arm64) when a `v*` tag is pushed, tagged with the release tag (e.g. `v0.2.1`).
A manual run (`workflow_dispatch`) pushes the tag name when started from a tag, and only the
commit SHA when started from a branch. Merging to `master` pushes no images, and there is no
`latest` tag:

- `ghcr.io/jfms7s/ticket-live-event-scanner-scraper`
- `ghcr.io/jfms7s/ticket-live-event-scanner-telegram-notifier`
- `ghcr.io/jfms7s/ticket-live-event-scanner-email-notifier`
- `ghcr.io/jfms7s/ticket-live-event-scanner-web-ui-api`
- `ghcr.io/jfms7s/ticket-live-event-scanner-web-ui-frontend`

`kustomization.yaml` pins every image to the newest release tag. To deploy another release,
bump the tag before applying:

```bash
cd deploy/k8s
kustomize edit set image \
  ticket-scanner/scraper=ghcr.io/jfms7s/ticket-live-event-scanner-scraper:<vX.Y.Z> \
  ticket-scanner/telegram-notifier=ghcr.io/jfms7s/ticket-live-event-scanner-telegram-notifier:<vX.Y.Z> \
  ticket-scanner/email-notifier=ghcr.io/jfms7s/ticket-live-event-scanner-email-notifier:<vX.Y.Z> \
  ticket-scanner/web-ui-api=ghcr.io/jfms7s/ticket-live-event-scanner-web-ui-api:<vX.Y.Z> \
  ticket-scanner/web-ui-frontend=ghcr.io/jfms7s/ticket-live-event-scanner-web-ui-frontend:<vX.Y.Z>
kubectl apply -k .
```

GHCR images are pushed as private packages by default. If your cluster doesn't have
`ghcr.io` pull access yet, create an `imagePullSecret` from a GitHub PAT with `read:packages`
scope and reference it from each Deployment/CronJob's `spec.template.spec.imagePullSecrets`,
or make the packages public in the repo's GitHub Package settings.

### Local development (KinD/minikube)

For iterating without pushing to GHCR, apply the raw manifests directly (bypassing kustomize's
image rewrite) with locally built images:

```bash
# Build images
docker build -t ticket-scanner/scraper:local -f cmd/scraper/Dockerfile .
docker build -t ticket-scanner/telegram-notifier:local -f cmd/telegram-notifier/Dockerfile .
docker build -t ticket-scanner/email-notifier:local -f cmd/email-notifier/Dockerfile .
docker build -t ticket-scanner/web-ui-api:local -f cmd/web-ui-api/Dockerfile .
docker build -t ticket-scanner/web-ui-frontend:local web/frontend

# Load into cluster
kind load docker-image ticket-scanner/scraper:local
kind load docker-image ticket-scanner/telegram-notifier:local
kind load docker-image ticket-scanner/email-notifier:local
kind load docker-image ticket-scanner/web-ui-api:local
kind load docker-image ticket-scanner/web-ui-frontend:local

# Apply each manifest directly, not via `-k`, so the :local tags aren't rewritten
kubectl apply -f namespace.yaml -f nats.yaml -f scraper.yaml \
  -f telegram-notifier.yaml -f email-notifier.yaml -f web-ui-api.yaml -f web-ui-frontend.yaml
```

## Secrets

Three Secrets are required before deployment:

### turso-credentials
libsql database connection details. The URL may point at hosted Turso or at any
self-hosted libsql-server (sqld), as `docker-compose.yml` does. Create with:
```bash
kubectl create secret generic turso-credentials \
  --from-literal=database_url='libsql://your-db-id.turso.io' \
  --from-literal=auth_token='your-turso-auth-token' \
  -n ticket-scanner
```

For hosted Turso, get these from your [Turso dashboard](https://app.turso.io).

### telegram-bot-token
Telegram Bot API credentials. Create with:
```bash
kubectl create secret generic telegram-bot-token \
  --from-literal=bot_token='YOUR_BOT_TOKEN_HERE' \
  --from-literal=chat_id='YOUR_CHAT_ID_HERE' \
  -n ticket-scanner
```

Get your bot token from [@BotFather](https://t.me/botfather), and your chat ID from your Telegram chat/channel.

### smtp-credentials
SMTP relay and addresses used by email-notifier for calendar invites. Create with:
```bash
kubectl create secret generic smtp-credentials \
  --from-literal=host='smtp.example.com' \
  --from-literal=port='587' \
  --from-literal=username='CHANGEME-SMTP-USERNAME' \
  --from-literal=password='CHANGEME-SMTP-PASSWORD' \
  --from-literal=from='notifier@example.com' \
  --from-literal=to='you@example.com' \
  -n ticket-scanner
```

**Do NOT commit real secrets to the repository.** The `secrets.example.yaml` file is for reference only and should NOT be applied directly — it contains placeholder values.

## Deploying

After creating the Secrets:

```bash
# Validate manifests (dry-run)
kubectl apply -k deploy/k8s --dry-run=client

# Deploy to cluster
kubectl apply -k deploy/k8s

# Verify deployment
kubectl get all -n ticket-scanner
kubectl logs -n ticket-scanner -l app=scraper
kubectl logs -n ticket-scanner -l app=telegram-notifier
kubectl logs -n ticket-scanner -l app=email-notifier
```

## NATS Streams

Each Go service calls `internal/streams.EnsureStreams()` on startup to idempotently create NATS JetStream streams and consumers. You do not need to manually configure NATS streams — they will be created automatically on first run.

## Architecture Notes

- **Cluster-internal only**: In these manifests the web UI services (API + frontend) are ClusterIP services with no Ingress. Access them from within the cluster (e.g., via port-forward for local testing).
- **Stateless scraper**: The scraper is a CronJob that runs once per hour. It queries the database to determine known event IDs and publishes only new discoveries.
- **Single NATS replica**: NATS is deployed as a single-replica StatefulSet for simplicity. It is self-hosted in-cluster per design (not a managed service).
- **External database**: The database is libsql (hosted Turso or a self-hosted libsql-server), not part of these manifests. Ensure the cluster can reach it.
