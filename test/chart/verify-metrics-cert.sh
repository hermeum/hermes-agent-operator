#!/usr/bin/env bash
# Checks that the Helm release issues and serves the metrics certificate.
# Run it after installing the chart with certManager.enabled and prometheus.enabled.
set -euo pipefail

NAMESPACE="${HELM_NAMESPACE:-hermes-agent}"
RELEASE="${HELM_RELEASE:-hermes-agent-operator}"
KUBECTL="${KUBECTL:-kubectl}"
CURL_IMAGE="${CURL_IMAGE:-quay.io/curl/curl:8.15.0}"
SELECTOR="app.kubernetes.io/instance=${RELEASE}"
PROBE="metrics-cert-probe"

command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }

echo "Waiting for the Certificate to be Ready"
"${KUBECTL}" wait --namespace "${NAMESPACE}" --for=condition=Ready --timeout=120s \
  certificate --selector "${SELECTOR}"

echo "Reading the ServiceMonitor"
monitor="$("${KUBECTL}" get servicemonitor --namespace "${NAMESPACE}" --selector "${SELECTOR}" -o json)"
secrets="$(jq -r '[.items[].spec.endpoints[].tlsConfig // empty | (.ca.secret.name, .cert.secret.name, .keySecret.name) | select(. != null)] | unique | .[]' <<<"${monitor}")"
host="$(jq -r '.items[0].spec.endpoints[0].tlsConfig.serverName' <<<"${monitor}")"
ca_secret="$(jq -r '.items[0].spec.endpoints[0].tlsConfig.ca.secret.name' <<<"${monitor}")"
ca_key="$(jq -r '.items[0].spec.endpoints[0].tlsConfig.ca.secret.key' <<<"${monitor}")"
if [[ -z "${secrets}" || "${host}" == "null" || "${ca_secret}" == "null" ]]; then
  echo "The ServiceMonitor does not set a serverName or does not reference a CA Secret" >&2
  exit 1
fi

echo "Checking that every Secret named by the ServiceMonitor exists"
for secret in ${secrets}; do
  "${KUBECTL}" get secret "${secret}" --namespace "${NAMESPACE}" >/dev/null
  echo "Found Secret ${secret}"
done

echo "Waiting for the manager rollout"
"${KUBECTL}" rollout status deployment --namespace "${NAMESPACE}" --selector "${SELECTOR}" --timeout=120s

port="$("${KUBECTL}" get service --namespace "${NAMESPACE}" --selector "${SELECTOR}" -o jsonpath='{.items[0].spec.ports[0].port}')"
url="https://${host}:${port}/metrics"
ca="$("${KUBECTL}" get secret "${ca_secret}" --namespace "${NAMESPACE}" -o go-template="{{index .data \"${ca_key}\" | base64decode}}")"

# Any HTTP status is fine here. The TLS handshake and hostname check come
# first, so a verification failure makes curl exit non-zero and fails the script.
echo "Verifying the certificate served by ${url}"
"${KUBECTL}" delete pod "${PROBE}" --namespace "${NAMESPACE}" --ignore-not-found --wait >/dev/null
"${KUBECTL}" run "${PROBE}" --namespace "${NAMESPACE}" --rm --attach --quiet \
  --restart=Never --pod-running-timeout=2m --image "${CURL_IMAGE}" \
  --env "CA=${ca}" --env "URL=${url}" --command -- \
  sh -c 'printf "%s\n" "$CA" >/tmp/ca.crt && curl --silent --show-error --output /dev/null --write-out "HTTP %{http_code}\n" --max-time 20 --retry 5 --retry-connrefused --retry-delay 2 --cacert /tmp/ca.crt "$URL"'
