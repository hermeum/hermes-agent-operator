---
name: hermes-agent-operator
description: >
  Scaffold a complete, commented HermesAgent custom resource YAML manifest.
  Reads the HermesAgent CRD schema from the live cluster (or falls back to the
  repo CRD file) so the output always matches the deployed spec version.
---

# Scaffold a HermesAgent Manifest

When this skill is invoked, generate a ready-to-apply HermesAgent custom resource YAML by following these steps in order.

## Step 1 — Fetch the CRD schema

Try each source in order, stopping at the first success:

**1. Live cluster (preferred):**
```
kubectl get crd hermesagents.agents.hermeum.app -o yaml 2>/dev/null
```
Use the `spec.versions[0].schema.openAPIV3Schema` block as the authoritative field reference.

**2. GitHub (fallback when kubectl is unavailable or the CRD is not installed):**

Fetch:
```
https://raw.githubusercontent.com/hermeum/hermes-agent-operator/main/skills/hermes-agent-operator/crd.yaml
```
This file is kept in sync with the operator's CRD and lives alongside this skill.

Use the schema only to understand field semantics and inline comment text. Do **not** dump the raw schema into the output.

## Step 2 — Ask the user for inputs

Collect the following, accepting Enter/blank for optional fields:

| Input | Required | Notes |
|-------|----------|-------|
| Agent name (`metadata.name`) | Yes | lowercase DNS label |
| Namespace | No | default: `default` |
| Image tag (`spec.hermes.image.tag`) | No | default: `latest` — pin to a specific release in production |
| Model provider | Yes | e.g. `anthropic`, `openai`, `ollama-cloud` |
| Model name | Yes | e.g. `claude-sonnet-4-6`, `gpt-4o`, `kimi-k2.6` |
| Model base URL | No | leave blank for provider default; required for `ollama`/`ollama-cloud` |
| API key Secret name | No | name of the k8s Secret whose keys become `$HERMES_HOME/.env` entries |
| Enable persistence | No | yes/no — default no; if yes, ask for size (default `10Gi`) |
| Enable SearXNG sidecar (web search) | No | yes/no |
| Enable Camofox sidecar (browser automation) | No | yes/no |
| Skills to pre-install | No | comma-separated identifiers, e.g. `anthropic-skills/code-review` |
| Cron schedule | No | name + schedule + prompt, e.g. `daily-summary, 0 9 * * *, summarize overnight alerts` |
| Create a ServiceAccount | No | yes/no — default yes |

If the user already provided some of these in the invocation message, use those values and skip asking for them.

## Step 3 — Emit the manifest

Output a single, self-contained YAML document (or multi-document YAML separated by `---` if a Secret is also needed).

Rules:
- **Include only fields the user asked for** — do not add optional sections the user did not request.
- **Every non-obvious field gets an inline `#` comment** derived from the CRD `description` property for that field.
- **Follow the minimal_spec pattern** for the base shape; extend it for each enabled option.
- If an API key Secret is needed, emit the Secret document first, separated from the HermesAgent by `---`.
- If the user enabled SearXNG or Camofox, include the corresponding `spec.searxng` or `spec.camofox` block with `{}` (empty, uses defaults) unless the user gave specific values.
- If skills were requested, include `spec.hermes.skills` as a list of `identifier:` entries.
- If a cron was requested, include `spec.hermes.crons` with the parsed name, schedule, and prompt.

### Minimal shape to build from

```yaml
apiVersion: agents.hermeum.app/v1alpha1
kind: HermesAgent
metadata:
  name: <name>
  namespace: <namespace>
spec:
  hermes:
    image:
      tag: <tag>          # pin to a release tag; "latest" is convenient but not reproducible
    config:
      raw:
        model:
          provider: <provider>
          default: <model>
          # base_url: https://...   # required for ollama / ollama-cloud
    workspace:
      dotEnv:
        secretRef:
          name: <secret-name>   # Secret keys become KEY=VALUE lines in $HERMES_HOME/.env
```

### Optional sections — add when user requested

**Persistence:**
```yaml
    storage:
      persistence:
        enabled: true
        size: 10Gi          # storage request for the PVC; omit to use the 10 Gi default
```

**ServiceAccount:**
```yaml
  security:
    rbac:
      createServiceAccount: true   # operator creates a dedicated ServiceAccount for this agent
```

**Skills:**
```yaml
    skills:
      - identifier: anthropic-skills/code-review   # skill identifier or HTTPS URL to a SKILL.md
```

**Cron:**
```yaml
    crons:
      - name: daily-summary
        schedule: "0 9 * * *"    # standard cron or shorthand like "every 30m"
        prompt: |
          Summarize overnight alerts and post a digest.
```

**SearXNG sidecar:**
```yaml
  searxng: {}   # enables the SearXNG web-search sidecar with defaults
```

**Camofox sidecar:**
```yaml
  camofox: {}   # enables the Camofox browser-automation sidecar with defaults
```

## Step 4 — Offer to save

After printing the YAML, ask:

> Save to a file? Enter a filename (e.g. `my-agent.yaml`) or press Enter to skip.

If the user provides a filename, write the YAML to that file using the Write tool.
