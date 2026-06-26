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

**2. Bundled CRD (fallback when kubectl is unavailable or the CRD is not installed):**

Read `crd.yaml` from the same directory as this skill file.
It is kept in sync with the operator's CRD and ships alongside this skill.

Use the schema only to understand field semantics and inline comment text. Do **not** dump the raw schema into the output.

## Step 2 — Understand the goal and collect inputs conversationally

### Step 2a — Ask what they want to build

Ask a single open-ended question first:

> "What would you like this agent to do? (e.g. 'a coding assistant that reviews PRs', 'a scheduled agent that summarizes Slack alerts')"

Use the answer to infer likely needs:
- **Persistence** — agents that accumulate context or files across runs
- **SearXNG** (web search) — research agents, news summarizers
- **Camofox** (browser automation) — agents that interact with web UIs
- **Cron schedule** — agents described as "daily", "periodic", "scheduled"
- **Model suggestion** — default to a sensible provider/model for the use case

### Step 2b — Ask for required specs

After the user describes their intent, ask only for the model choice:

1. **Model provider + model name** — suggest a sensible default based on their description (e.g. `anthropic` / `claude-sonnet-4-6`); confirm or let them pick another

> **Tip:** For the `spec.hermes.config.raw` block, the `hermes-agent` skill knows the full Hermes configuration schema. Invoke it if you need help generating a detailed config beyond the minimal model settings.

Everything else is handled automatically:

- **Agent name** — derive a concise, lowercase DNS label from the description (e.g. `pr-review-agent`, `slack-digest`). No need to ask.
- **API key Secret** — infer the required key name(s) from the chosen provider (e.g. `ANTHROPIC_API_KEY` for `anthropic`, `OPENAI_API_KEY` for `openai`). Name the Secret `<agent-name>-credentials` and emit it as part of the manifest with the correct key(s) set to empty string (`""`). The user fills in the values after applying.

If the user already provided any of these in their original message, use those values instead.

### Step 2c — Ask about optional features only when relevant

Do **not** show a blanket list of optional features every time. Instead, based on the description from Step 2a, ask only about the features that make sense for what the user described:

- **persistence** — ask only if the agent accumulates files, memory, or state across runs
- **SearXNG** (web search) — ask only if the agent needs live web or news data
- **Camofox** (browser) — ask only if the agent needs to interact with a web UIs
- **cron** — ask only if the agent is described as periodic, scheduled, or recurring
- **skills** — ask only if the agent is a Claude Code agent or the user mentioned specific capabilities
- **init scripts / packages** — ask only if the agent needs external CLIs, SDKs, or binaries installed before it starts (e.g. "upload to Box", "run AWS CLI", "needs Python requests")
- **namespace** — only ask if the user mentioned a specific namespace or multi-tenant setup

If none of these signals are present in the description, skip Step 2c entirely and proceed with the minimal spec.

When you do ask, ask about each relevant option individually (one question at a time), not as a bulk list. Collect just the details needed to fill that section:
- **persistence** → storage size (default `10Gi`)
- **cron** → schedule (e.g. `0 9 * * *`) and prompt text
- **skills** → comma-separated skill identifiers (e.g. `anthropic-skills/code-review`)
- **init scripts** → what to install (e.g. Box CLI via npm, a specific SDK). Use `spec.hermes.initScripts` for simple shell scripts and `spec.hermes.packages` for pip/npm packages.
- **namespace** → namespace name
- **ServiceAccount** is created by default; only raise it if the user mentions RBAC or service accounts

### Field reference (for generating the manifest — do not present this as a form to the user)

Example manifests are available in the `examples/` directory alongside this skill file. Read a relevant one if you need a structural pattern for the feature the user is asking for — load only the specific file you need, not all of them.

| Field | Required | Notes |
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
| Init scripts | No | shell scripts run before agent starts; use for installing CLIs/SDKs |
| Packages (pip/npm) | No | pre-install language packages before the agent starts |
| Create a ServiceAccount | No | yes/no — default yes |

## Step 3 — Emit the manifest

Output a single, self-contained YAML document (or multi-document YAML separated by `---` if a Secret is also needed).

Rules:
- **Include only fields the user asked for** — do not add optional sections the user did not request.
- **Every non-obvious field gets an inline `#` comment** derived from the CRD `description` property for that field.
- **Follow the minimal_spec pattern** for the base shape; extend it for each enabled option.
- **Always emit the credentials Secret first**, separated from the HermesAgent by `---`. Name it `<agent-name>-credentials`, include the provider's required env key(s) with empty string values, and add a comment telling the user to fill them in before applying (e.g. `# kubectl edit secret <name>` or `# fill in before applying`).
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

**Init scripts (install CLIs/SDKs before the agent starts):**
```yaml
    initScripts:
      - name: install-box-cli
        script: |
          npm install -g box
```

**Packages (pre-install via pip/npm):**
```yaml
    packages:
      npm:
        - box                  # Box Node SDK + CLI
      pip:
        - boxsdk>=10           # Box Python SDK
```

## Step 4 — Offer to save

After printing the YAML, ask:

> Save to a file? Enter a filename (e.g. `my-agent.yaml`) or press Enter to skip.

If the user provides a filename, write the YAML to that file using the Write tool.

## References

- `references/box-integration.md` — how to scaffold Box skill + CLI + credentials for agents that upload files to Box.
