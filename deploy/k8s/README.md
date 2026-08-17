# Kubernetes Manifests for Ticket Scanner

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
- **web-ui-api.yaml**: Web API backend (Deployment + Service)
  - Go JSON API on port 8080 (exposed via Service on port 80)
  - Liveness/readiness probes on `GET /healthz`
  - Consumes all three NATS streams, materializes into Turso
- **web-ui-frontend.yaml**: Web UI frontend (Deployment + Service)
  - JavaScript/frontend server on port 3000 (exposed via Service on port 80)
  - Calls `web-ui-api` internally
  - Cluster-internal only; no public Ingress
- **secrets.example.yaml**: Placeholder Secret templates (see "Secrets" section below)
- **kustomization.yaml**: Kustomize index, includes all resources

## Image Placeholders

All image references use placeholders:
- `ticket-scanner/scraper:local`
- `ticket-scanner/telegram-notifier:local`
- `ticket-scanner/web-ui-api:local`
- `ticket-scanner/web-ui-frontend:local`

**Before applying**, you must:
1. Build each service's container image from its Dockerfile (`cmd/scraper`, `cmd/telegram-notifier`, `cmd/web-ui`, `web/frontend`)
2. Push images to a registry, or load them into your local cluster (e.g., `kind load docker-image` or `minikube image load`)
3. Update image references in the manifests to match your registry/tag

Example for local development (KinD cluster):
```bash
# Build images
docker build -t ticket-scanner/scraper:local -f cmd/scraper/Dockerfile .
docker build -t ticket-scanner/telegram-notifier:local -f cmd/telegram-notifier/Dockerfile .
docker build -t ticket-scanner/web-ui-api:local -f cmd/web-ui/Dockerfile .
docker build -t ticket-scanner/web-ui-frontend:local -f web/frontend/Dockerfile .

# Load into cluster
kind load docker-image ticket-scanner/scraper:local
kind load docker-image ticket-scanner/telegram-notifier:local
kind load docker-image ticket-scanner/web-ui-api:local
kind load docker-image ticket-scanner/web-ui-frontend:local
```

## Secrets

Two Secrets are required before deployment:

### turso-credentials
Turso database connection details. Create with:
```bash
kubectl create secret generic turso-credentials \
  --from-literal=database_url='libsql://your-db-id.turso.io' \
  --from-literal=auth_token='your-turso-auth-token' \
  -n ticket-scanner
```

Get these from your [Turso dashboard](https://app.turso.io).

### telegram-bot-token
Telegram Bot API credentials. Create with:
```bash
kubectl create secret generic telegram-bot-token \
  --from-literal=bot_token='YOUR_BOT_TOKEN_HERE' \
  --from-literal=chat_id='YOUR_CHAT_ID_HERE' \
  -n ticket-scanner
```

Get your bot token from [@BotFather](https://t.me/botfather), and your chat ID from your Telegram chat/channel.

**Do NOT commit real secrets to the repository.** The `secrets.example.yaml` file is for reference only and should NOT be applied directly — it contains placeholder values.

## Deploying

After creating the Secrets and updating image references:

```bash
# Validate manifests (dry-run)
kubectl apply -k deploy/k8s --dry-run=client

# Deploy to cluster
kubectl apply -k deploy/k8s

# Verify deployment
kubectl get all -n ticket-scanner
kubectl logs -n ticket-scanner -l app=scraper
kubectl logs -n ticket-scanner -l app=telegram-notifier
```

## NATS Streams

Each Go service calls `internal/streams.EnsureStreams()` on startup to idempotently create NATS JetStream streams and consumers. You do not need to manually configure NATS streams — they will be created automatically on first run.

## Architecture Notes

- **Cluster-internal only**: The web UI services (API + frontend) are ClusterIP services with no public Ingress. Access them from within the cluster (e.g., via port-forward for local testing).
- **Stateless scraper**: The scraper is a CronJob that runs once per hour. It queries Turso to determine known event IDs and publishes only new discoveries.
- **Single NATS replica**: NATS is deployed as a single-replica StatefulSet for simplicity. It is self-hosted in-cluster per design (not a managed service).
- **Turso as external dependency**: The database is Turso (hosted service), not an in-cluster database. Ensure your cluster has outbound internet access to Turso.
