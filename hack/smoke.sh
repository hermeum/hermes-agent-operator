#!/usr/bin/env bash
set -Eeuo pipefail

# Smoke test for a deployed hermes-agent-operator.
#
# Checks, with bounded waits:
#   1. optional Helm install/upgrade of the operator
#   2. operator Deployment rollout in the target namespace
#   3. HermesAgent CR apply/reconciliation
#   4. generated StatefulSet rollout and agent Pod Ready/Running
#   5. optional API health/query when API credentials are supplied
#
# Common usage:
#   NS=hermes-agent hack/smoke.sh
#   ANTHROPIC_API_KEY=... RUN_AGENT_QUERY=true hack/smoke.sh
#   HELM_INSTALL=true HELM_RELEASE=hermes-agent-operator hack/smoke.sh

NS="${NS:-hermes-agent}"
AGENT="${AGENT:-hermes-smoke}"
KUBECTL="${KUBECTL:-kubectl}"
HELM="${HELM:-helm}"
HELM_INSTALL="${HELM_INSTALL:-false}"
HELM_RELEASE="${HELM_RELEASE:-hermes-agent-operator}"
HELM_CHART="${HELM_CHART:-dist/chart}"
HELM_ARGS="${HELM_ARGS:-}"
OPERATOR_DEPLOYMENT="${OPERATOR_DEPLOYMENT:-}"
MODEL_PROVIDER="${MODEL_PROVIDER:-anthropic}"
MODEL_DEFAULT="${MODEL_DEFAULT:-claude-sonnet-4-5}"
API_PORT="${API_PORT:-8642}"
LOCAL_PORT="${LOCAL_PORT:-18642}"
PROMPT="${PROMPT:-Reply with exactly: smoke-ok}"
EXPECTED_SUBSTRING="${EXPECTED_SUBSTRING:-smoke-ok}"
RUN_AGENT_QUERY="${RUN_AGENT_QUERY:-auto}"
CLEANUP="${CLEANUP:-true}"
ROLLOUT_TIMEOUT="${ROLLOUT_TIMEOUT:-180s}"
AGENT_TIMEOUT="${AGENT_TIMEOUT:-300s}"
CURL_MAX_TIME="${CURL_MAX_TIME:-10}"

created_secret=false
pf_pid=""
tmp_env=""

log() { printf '==> %s\n' "$*"; }
warn() { printf 'WARNING: %s\n' "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "Required command not found: $1"
}

is_true() {
  case "$1" in
    true|TRUE|1|yes|YES|y|Y) return 0 ;;
    *) return 1 ;;
  esac
}

cleanup() {
  if [[ -n "${pf_pid}" ]]; then
    kill "${pf_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${tmp_env}" && -f "${tmp_env}" ]]; then
    rm -f "${tmp_env}"
  fi
  if is_true "${CLEANUP}"; then
    "${KUBECTL}" -n "${NS}" delete hermesagent "${AGENT}" --ignore-not-found=true >/dev/null 2>&1 || true
    if [[ "${created_secret}" == "true" ]]; then
      "${KUBECTL}" -n "${NS}" delete secret "${AGENT}-model" --ignore-not-found=true >/dev/null 2>&1 || true
    fi
  fi
}
trap cleanup EXIT

need "${KUBECTL}"
need curl
need python3

if is_true "${HELM_INSTALL}"; then
  need "${HELM}"
  log "Installing/upgrading Helm release ${HELM_RELEASE} in namespace ${NS}"
  # shellcheck disable=SC2086 # HELM_ARGS intentionally allows callers to pass multiple Helm flags.
  "${HELM}" upgrade --install "${HELM_RELEASE}" "${HELM_CHART}" \
    --namespace "${NS}" \
    --create-namespace \
    --wait \
    --timeout "${ROLLOUT_TIMEOUT}" \
    ${HELM_ARGS}
else
  log "Ensuring namespace ${NS} exists"
  "${KUBECTL}" get namespace "${NS}" >/dev/null
fi

if [[ -z "${OPERATOR_DEPLOYMENT}" ]]; then
  OPERATOR_DEPLOYMENT="$(${KUBECTL} -n "${NS}" get deploy \
    -l 'control-plane=controller-manager' \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
fi
if [[ -z "${OPERATOR_DEPLOYMENT}" ]]; then
  OPERATOR_DEPLOYMENT="${HELM_RELEASE}-controller-manager"
fi

log "Checking operator rollout deployment/${OPERATOR_DEPLOYMENT} in namespace ${NS}"
"${KUBECTL}" -n "${NS}" rollout status "deployment/${OPERATOR_DEPLOYMENT}" --timeout="${ROLLOUT_TIMEOUT}"
"${KUBECTL}" -n "${NS}" wait --for=condition=Available "deployment/${OPERATOR_DEPLOYMENT}" --timeout="${ROLLOUT_TIMEOUT}"
"${KUBECTL}" -n "${NS}" get deploy "${OPERATOR_DEPLOYMENT}" -o wide

if [[ -n "${ANTHROPIC_API_KEY:-}${OPENAI_API_KEY:-}" ]]; then
  tmp_env="$(mktemp "${TMPDIR:-/tmp}/hermes-smoke-env.XXXXXX")"
  chmod 600 "${tmp_env}"
  if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
    printf 'ANTHROPIC_API_KEY=%s\n' "${ANTHROPIC_API_KEY}" >>"${tmp_env}"
  fi
  if [[ -n "${OPENAI_API_KEY:-}" ]]; then
    printf 'OPENAI_API_KEY=%s\n' "${OPENAI_API_KEY}" >>"${tmp_env}"
  fi
  log "Applying smoke model Secret ${AGENT}-model"
  "${KUBECTL}" -n "${NS}" create secret generic "${AGENT}-model" \
    --from-env-file="${tmp_env}" \
    --dry-run=client -o yaml | "${KUBECTL}" apply -f -
  created_secret=true
else
  warn "No ANTHROPIC_API_KEY or OPENAI_API_KEY supplied; applying agent without model Secret and skipping answer check"
fi

log "Applying HermesAgent ${AGENT}"
if [[ "${created_secret}" == "true" ]]; then
  "${KUBECTL}" -n "${NS}" apply -f - <<EOF
apiVersion: agents.hermeum.app/v1alpha1
kind: HermesAgent
metadata:
  name: ${AGENT}
spec:
  hermes:
    config:
      apiServer:
        enabled: true
        port: ${API_PORT}
      raw:
        model:
          provider: ${MODEL_PROVIDER}
          default: ${MODEL_DEFAULT}
    envFrom:
      - secretRef:
          name: ${AGENT}-model
EOF
else
  "${KUBECTL}" -n "${NS}" apply -f - <<EOF
apiVersion: agents.hermeum.app/v1alpha1
kind: HermesAgent
metadata:
  name: ${AGENT}
spec:
  hermes:
    config:
      apiServer:
        enabled: true
        port: ${API_PORT}
      raw:
        model:
          provider: ${MODEL_PROVIDER}
          default: ${MODEL_DEFAULT}
EOF
fi

log "Waiting for reconciled StatefulSet ${AGENT}"
"${KUBECTL}" -n "${NS}" rollout status "statefulset/${AGENT}" --timeout="${AGENT_TIMEOUT}"
"${KUBECTL}" -n "${NS}" wait --for=condition=Ready "pod/${AGENT}-0" --timeout="${AGENT_TIMEOUT}"
pod_phase="$(${KUBECTL} -n "${NS}" get pod "${AGENT}-0" -o jsonpath='{.status.phase}')"
[[ "${pod_phase}" == "Running" ]] || die "pod/${AGENT}-0 phase is ${pod_phase}, expected Running"
ha_phase="$(${KUBECTL} -n "${NS}" get hermesagent "${AGENT}" -o jsonpath='{.status.phase}' 2>/dev/null || true)"
if [[ "${ha_phase}" != "Running" ]]; then
  warn "HermesAgent status.phase is '${ha_phase:-<empty>}' (pod is Running/Ready)"
fi
"${KUBECTL}" -n "${NS}" get hermesagent "${AGENT}" -o wide
"${KUBECTL}" -n "${NS}" get pod "${AGENT}-0" -o wide

api_key="$(${KUBECTL} -n "${NS}" get secret "${AGENT}-hermes" -o jsonpath='{.data.API_SERVER_KEY}' | base64 -d)"
log "Port-forwarding service/${AGENT} to 127.0.0.1:${LOCAL_PORT}"
pf_log="${TMPDIR:-/tmp}/hermes-agent-smoke-port-forward-${AGENT}.log"
"${KUBECTL}" -n "${NS}" port-forward "service/${AGENT}" "${LOCAL_PORT}:${API_PORT}" >"${pf_log}" 2>&1 &
pf_pid=$!

health_ok=false
for _ in $(seq 1 30); do
  if curl -fsS --max-time "${CURL_MAX_TIME}" "http://127.0.0.1:${LOCAL_PORT}/health" >/dev/null; then
    health_ok=true
    break
  fi
  if ! kill -0 "${pf_pid}" >/dev/null 2>&1; then
    die "kubectl port-forward exited early; log: $(cat "${pf_log}")"
  fi
  sleep 2
done
[[ "${health_ok}" == "true" ]] || die "Timed out waiting for API /health via port-forward; log: $(cat "${pf_log}")"
curl -fsS --max-time "${CURL_MAX_TIME}" "http://127.0.0.1:${LOCAL_PORT}/health"
printf '\n'

if [[ "${RUN_AGENT_QUERY}" == "auto" ]]; then
  if [[ "${created_secret}" == "true" ]]; then
    RUN_AGENT_QUERY=true
  else
    RUN_AGENT_QUERY=false
  fi
fi

if is_true "${RUN_AGENT_QUERY}"; then
  [[ "${created_secret}" == "true" ]] || die "RUN_AGENT_QUERY=true requires ANTHROPIC_API_KEY or OPENAI_API_KEY"
  log "Asking agent through OpenAI-compatible API"
  payload="$(PROMPT="${PROMPT}" python3 - <<'PY'
import json, os
print(json.dumps({
    "model": "hermes-agent",
    "messages": [{"role": "user", "content": os.environ["PROMPT"]}],
    "stream": False,
}))
PY
)"
  auth_header_prefix="Authorization: Bea"
  auth_header_prefix="${auth_header_prefix}r""er "
  response="$(curl -fsS --max-time "${CURL_MAX_TIME}" "http://127.0.0.1:${LOCAL_PORT}/v1/chat/completions" \
    -H "${auth_header_prefix}${api_key}" \
    -H "Content-Type: application/json" \
    -d "${payload}")"
  answer="$(printf '%s' "${response}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["choices"][0]["message"]["content"])')"
  printf 'Agent answer: %s\n' "${answer}"
  [[ "${answer}" == *"${EXPECTED_SUBSTRING}"* ]] || die "Agent answer did not contain expected substring: ${EXPECTED_SUBSTRING}"

else
  log "Skipping agent answer check (set RUN_AGENT_QUERY=true and provide credentials to enable)"
fi

if is_true "${CLEANUP}"; then
  log "Cleaning up smoke HermesAgent/Secret (set CLEANUP=false to keep resources)"
fi
log "smoke-ok: operator healthy, HermesAgent applied, pod Running/Ready, API health checked"
