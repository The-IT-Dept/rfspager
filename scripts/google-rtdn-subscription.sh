#!/usr/bin/env bash
# Point Google Play real-time developer notifications (RTDN) at the Go server.
#
#   scripts/google-rtdn-subscription.sh <topic>
#
# Play Console → Monetise with Play → Monetisation setup → Real-time developer
# notifications names a Pub/Sub topic (projects/<project>/topics/<name>); run
# without arguments to list the topics in the project. The script adds (or
# updates) a push subscription on that topic that delivers to the Go webhook
# with an OIDC token minted for the rfspager-go service account, which the
# server verifies against GOOGLE_PUBSUB_AUDIENCE / GOOGLE_PUBSUB_SERVICE_EMAIL
# in /opt/rfspager-go/.env (both set on apps01 on 2026-10-15).
#
# Needs `gcloud auth login` as a project owner or editor; the server's own
# service accounts cannot manage Pub/Sub. Idempotent.
set -euo pipefail

PROJECT="${PROJECT:-rfspager-e1e40}"
ENDPOINT="${ENDPOINT:-https://rfspager.app/api/webhooks/google}"
SA="${SA:-rfspager-go@${PROJECT}.iam.gserviceaccount.com}"
SUB="${SUB:-rfspager-go-rtdn}"
TOPIC="${1:-}"

if [ -z "$TOPIC" ]; then
  echo "Pub/Sub topics in $PROJECT:"
  gcloud pubsub topics list --project "$PROJECT" --format='value(name)'
  echo
  echo "usage: $0 <topic>   (the one named in Play Console under RTDN)" >&2
  exit 1
fi

# Pub/Sub's service agent must be allowed to mint tokens as the service account.
PROJECT_NUMBER="$(gcloud projects describe "$PROJECT" --format='value(projectNumber)')"
gcloud iam service-accounts add-iam-policy-binding "$SA" --project "$PROJECT" \
  --member="serviceAccount:service-${PROJECT_NUMBER}@gcp-sa-pubsub.iam.gserviceaccount.com" \
  --role=roles/iam.serviceAccountTokenCreator -q >/dev/null

if gcloud pubsub subscriptions describe "$SUB" --project "$PROJECT" >/dev/null 2>&1; then
  gcloud pubsub subscriptions update "$SUB" --project "$PROJECT" \
    --push-endpoint="$ENDPOINT" --push-auth-service-account="$SA" --push-auth-token-audience="$ENDPOINT"
else
  gcloud pubsub subscriptions create "$SUB" --project "$PROJECT" --topic="$TOPIC" \
    --push-endpoint="$ENDPOINT" --push-auth-service-account="$SA" --push-auth-token-audience="$ENDPOINT" \
    --ack-deadline=30 --min-retry-delay=10s --max-retry-delay=600s
fi

gcloud pubsub subscriptions describe "$SUB" --project "$PROJECT" \
  --format='value(topic,pushConfig.pushEndpoint,pushConfig.oidcToken.serviceAccountEmail)'

cat <<MSG

Done. To exercise the path, publish Play's test-notification shape:
  gcloud pubsub topics publish $TOPIC --project $PROJECT \\
    --message='{"version":"1.0","packageName":"io.devlan.rfspager","eventTimeMillis":"0","testNotification":{"version":"1.0"}}'
then on apps01:
  journalctl -u rfspager-go --since '5 min ago' -o cat | grep 'google webhook'
Any other subscription on the topic (Adapty's) can be deleted once this one is receiving.
MSG
