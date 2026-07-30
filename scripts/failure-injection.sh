#!/usr/bin/env bash
set -euo pipefail

if [[ "${NEXDROP_INTEGRATION_DISPOSABLE:-}" != "true" ]]; then
  echo 'NEXDROP_INTEGRATION_DISPOSABLE=true is required; failure injection is destructive' >&2
  exit 2
fi

scenario="${1:-}"
base_url="${NEXDROP_INTEGRATION_BASE_URL:-http://127.0.0.1:8080}"

case "$scenario" in
  deterministic)
    "$0" network-faults
    "$0" storage-faults
    "$0" client-constraints
    "$0" mixed-clients
    "$0" worker-recovery
    ;;
  network-faults)
    go test -count=1 ./internal/failureinject -run 'TestPacketLoss|TestAddressFamily'
    go test -count=1 ./internal/filetransfer -run 'TestUploadConnectionReset|TestDownloadReset'
    go test -count=1 ./internal/domain -run 'TestSelectRoute'
    npm --prefix web test
    ;;
  storage-faults)
    go test -count=1 ./internal/filetransfer -run 'TestSlowStorage|TestStorageRecordFailure|TestUploadRejects'
    if [[ -n "${NEXDROP_TEST_DATABASE_URL:-}" ]]; then
      go test -count=1 ./internal/postgres -run TestTransferAndFileFlowIntegration
    else
      echo 'NEXDROP_TEST_DATABASE_URL is not set; storage-full integration scenario skipped' >&2
    fi
    ;;
  client-constraints)
    go test -count=1 ./internal/failureinject -run TestReceiverPauseTerminationAndReconnect
    go test -count=1 ./internal/transfer -run TestReportTimelineEvent
    ;;
  mixed-clients)
    go test -count=1 ./internal/version
    npm --prefix web test
    ;;
  worker-recovery)
    go test -count=1 ./internal/failureinject -run TestCleanupAndIdempotentReplayDuringActiveTransfer
    go test -count=1 ./internal/maintenance ./internal/presence
    if [[ -n "${NEXDROP_TEST_DATABASE_URL:-}" ]]; then
      go test -count=1 ./internal/postgres -run TestTransferAndFileFlowIntegration
    else
      echo 'NEXDROP_TEST_DATABASE_URL is not set; concurrent retry-replay scenario skipped' >&2
    fi
    ;;
  database-outage)
    container="${NEXDROP_POSTGRES_CONTAINER:?NEXDROP_POSTGRES_CONTAINER is required}"
    docker stop "$container" >/dev/null
    trap 'docker start "$container" >/dev/null 2>&1 || true' EXIT
    status="$(curl --max-time 5 --silent --output /dev/null --write-out '%{http_code}' "$base_url/readyz" || true)"
    [[ "$status" == "503" ]]
    docker start "$container" >/dev/null
    ready=false
    for _ in $(seq 1 30); do
      if curl --connect-timeout 1 --max-time 2 --fail --silent "$base_url/readyz" >/dev/null; then
        ready=true
        break
      fi
      sleep 1
    done
    [[ "$ready" == "true" ]]
    trap - EXIT
    ;;
  *)
    echo 'usage: failure-injection.sh deterministic|network-faults|storage-faults|client-constraints|mixed-clients|worker-recovery|database-outage' >&2
    exit 2
    ;;
esac
