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
- **web-ui-api.yaml**: Web API backend (Deployment + Service)
  - Go JSON API on port 8080 (exposed via Service on port 80)
  - Liveness/readiness probes on `GET /healthz`
  - Consumes both NATS streams (`EVENTS`, `NOTIFICATIONS`), materializes into its SQLite database (`/data/ticket-scanner.db` on the `web-ui-api-data` volume)
  - Also runs the scraper (a scrape at start-up and then every hour, pulling ticketline.pt and publishing to NATS `events.discovered`) and the weekly digest (Sunday 19:00 Lisbon); `SCRAPER_ENABLED=false` turns both off. Scrape by hand: `kubectl exec -n ticket-scanner deploy/web-ui-api -- /app/web-ui-api --scrape-once`
  - Also delivers the notifications: Telegram (each new session, the weekly digest) and email calendar invites, from the `telegram-bot-token` and `smtp-credentials` Secrets. A channel whose Secret is missing is disabled, not fatal, and shows as 0 in `ticket_scanner_notifier_enabled` on `/metrics`
  - Also serves the dashboard (the React build is embedded in the binary) at `/`, same origin as `/api`
  - These manifests define no Ingress
- **networkpolicy.yaml**: Ingress NetworkPolicies (egress is not restricted)
  - Default-deny ingress for every pod in the namespace
  - NATS `4222` only from `web-ui-api` pods
  - `web-ui-api` `8080` from pods in this namespace and from `kube-system` (k3s Traefik)
  - The services accept optional NATS credentials (`NATS_TOKEN`, `NATS_USER` + `NATS_PASSWORD`, or `NATS_CREDS`); this broker runs without auth
- **secrets.example.yaml**: Placeholder Secret templates (see "Secrets" section below)
- **kustomization.yaml**: Kustomize index, includes all resources

## Images

The raw manifests reference local placeholder image names (e.g. `ticket-scanner/web-ui-api:local`).
`kustomization.yaml` rewrites these at apply-time via its `images:` section, so
`kubectl apply -k deploy/k8s` always deploys the GHCR images, not the placeholders.

### GitHub Container Registry (default)

`.github/workflows/build-push.yml` builds and pushes all four images to GHCR (multi-arch,
amd64 + arm64) when a `v*` tag is pushed, tagged with the release tag (e.g. `v0.2.1`).
A manual run (`workflow_dispatch`) pushes the tag name when started from a tag, and only the
commit SHA when started from a branch. Merging to `master` pushes no images, and there is no
`latest` tag:

- `ghcr.io/jfms7s/ticket-live-event-scanner-web-ui-api`

`kustomization.yaml` pins every image to the newest release tag. To deploy another release,
bump the tag before applying:

```bash
cd deploy/k8s
kustomize edit set image \
  ticket-scanner/web-ui-api=ghcr.io/jfms7s/ticket-live-event-scanner-web-ui-api:<vX.Y.Z>
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
docker build -t ticket-scanner/web-ui-api:local -f cmd/web-ui-api/Dockerfile .

# Load into cluster
kind load docker-image ticket-scanner/web-ui-api:local

# Apply each manifest directly, not via `-k`, so the :local tags aren't rewritten
kubectl apply -f namespace.yaml -f nats.yaml \
  -f web-ui-api.yaml \
  -f networkpolicy.yaml
```

## Secrets

Two Secrets configure the notification channels (each is optional: a channel without its Secret is disabled). The database is a SQLite file on the `web-ui-api-data` volume and needs none. On a fresh install set `DATABASE_ALLOW_EMPTY=true` for the first start; an empty database is otherwise refused, since it is also what a wrong or unmounted volume looks like and every session would be announced again.

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
SMTP relay and addresses used by web-ui-api's email channel for calendar invites. Create with:
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
```

## Upgrading from separate notifier pods

Earlier releases ran `telegram-notifier` and `email-notifier` as their own
Deployments. `kubectl apply -k` does not delete what the manifests no longer
list, so remove them when moving to this layout; left running, they would share
the same durable consumers with web-ui-api and split the deliveries:

```bash
kubectl delete deployment telegram-notifier email-notifier -n ticket-scanner --ignore-not-found
```

## NATS Streams

Each Go service calls `internal/streams.EnsureStreams()` on startup to idempotently create NATS JetStream streams and consumers. You do not need to manually configure NATS streams — they will be created automatically on first run.

## Architecture Notes

- **Cluster-internal only**: In these manifests the web UI services (the API, which also serves the dashboard) are ClusterIP services with no Ingress. Access them from within the cluster (e.g., via port-forward for local testing).
- **Scraper in the API process**: web-ui-api runs the scraper on a ticker (hourly, plus once at start-up). It queries the database to determine known event IDs and publishes only new discoveries.
- **Single NATS replica**: NATS is deployed as a single-replica StatefulSet for simplicity. It is self-hosted in-cluster per design (not a managed service).
- **Database on a volume**: web-ui-api owns one SQLite file (WAL) on a ReadWriteOnce volume, so it runs as a single replica with `Recreate`.

## Upgrading from the scraper CronJobs

Earlier releases ran the scraper as a CronJob (and the digest as a second one). `kubectl apply -k` does not delete what the manifests no longer list, so remove them when moving to this layout; left running they would scrape beside the in-process ticker (harmless, the dedup holds, but wasteful):

```bash
kubectl delete cronjob scraper -n ticket-scanner --ignore-not-found
kubectl delete serviceaccount,role,rolebinding scraper -n ticket-scanner --ignore-not-found
```

The Helm chart (`ticket-live-event-scanner-helm` >= 0.13.0) also had a weekly digest CronJob (`<release>-ticket-scanner-digest`); Argo CD prunes it with the chart upgrade, but if you removed the chart by hand, delete it too. Until it is gone it would publish the digest with the old image (the week's single slot goes to whichever publishes first).
