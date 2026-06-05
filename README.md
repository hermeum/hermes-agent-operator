# hermes-agent-operator

<p align="center"><img alt="Hermes Gopher" src="./img/hermes-agent-gopher.png" width="250" height="250"/></p>

Self-hosting [Hermes agent](https://github.com/nousresearch/hermes-agent) on Kubernetes in a declarative, reproducible manner. 

## Why

Hermes agent is a powerful tool for automating tasks — but it is designed for personal use. Running it across a team is difficult: configurations drift, skills go out of sync, and there is no shared source of truth for what each agent does or what it has access to.

`hermes-agent-operator` solves this by managing Hermes agents as Kubernetes custom resources. You declare the full state of an agent — its config, workspace files, skills, crons, and bundles — in a single manifest. The operator keeps the running agent in sync with that declaration. 

## Quick Start

**Prerequisites:** a Kubernetes cluster and Helm v3.

### 1. Install the operator

```sh
helm install hermes-agent-operator oci://ghcr.io/noahingh/charts/hermes-agent-operator \
  --namespace hermes-agent --create-namespace
```

### 2. Create a secret with your API key

```sh
kubectl create secret generic my-hermes-secret \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-...
```

### 3. Deploy a HermesAgent

```sh
kubectl apply -f - <<EOF
apiVersion: agents.hermes-agent-operator.xyz/v1alpha1
kind: HermesAgent
metadata:
  name: my-agent
spec:
  hermes:
    config:
      raw:
        model:
          provider: anthropic
          default: claude-sonnet-4-6
    envFrom:
      - secretRef:
          name: my-hermes-secret
EOF
```

### 4. Verify

```sh
kubectl get hermesagent my-agent
kubectl get pods -l app.kubernetes.io/instance=my-agent
```

## Configuration

- [`hermes.config`](#hermesconfig)
- [`hermes.storage`](#hermesstorage)
- [`hermes.workspace`](#hermesworkspace)
- [`hermes.plugins`](#hermesplugins)
- [`hermes.skills`](#hermesskills)
- [`hermes.crons`](#hermescrons)
- [`hermes.bundles`](#hermesbundles)
- [`hermes.env` / `hermes.envFrom`](#hermesenv--hermesenvfrom)
- [`hermes.resources`](#hermesresources)
- [`hermes.initChownData`](#hermesinit​chowndata)
- [`searxng`](#searxng)
- [`camofox`](#camofox)
- [`security`](#security)
- [`networking`](#networking)
- [`suspend`](#suspend)

### `hermes.config`

Configure the Hermes agent runtime. `raw` and `apiServer` can be used independently or together.

**`raw`** — pass a verbatim `config.yml` as free-form YAML. Anything valid in a Hermes config file is accepted here.

```yaml
hermes:
  config:
    raw:                           # optional; omit if no custom config.yml is needed
      model:
        provider: anthropic
        default: claude-sonnet-4-6
```

**`apiServer`** — enable the built-in gateway API. The operator sets `API_SERVER_ENABLED=true` automatically and generates a Kubernetes Secret named `<agent-name>-hermes` containing `API_SERVER_KEY`, which is injected into the agent container automatically.

```yaml
hermes:
  config:
    apiServer:                     # optional; omit to disable the gateway API
      enabled: true
```

### `hermes.storage`

Persistent volume for agent data at `/opt/data`. Without persistence, data is lost on pod restart.

```yaml
hermes:
  storage:
    persistence:
      enabled: true
      size: 10Gi                   # optional; defaults to 10Gi
      storageClassName: standard   # optional; omit to use the cluster default StorageClass
      existingClaim: my-pvc        # optional; omit to provision a new PVC automatically
```


### `hermes.workspace`

Seed files into the agent's home directory before startup. Keys are relative paths; use `/` as a separator for subdirectories.

```yaml
hermes:
  workspace:
    files:                         # optional; omit if no files need to be seeded
      SOUL.md: |
        You are a pragmatic senior engineer.
      skills/custom/SKILL.md: |   # subdirectory path — operator creates parent dirs automatically
        # My Custom Skill
        ...
```


### `hermes.plugins`

Install Hermes plugins at startup. Use `owner/repo` shorthand or a full Git URL.

```yaml
hermes:
  plugins:                         # optional; omit if no plugins are needed
    - identifier: hermes-agent/plugin-stocks  # required; owner/repo or full Git URL
      enable: true                 # optional; defaults to true (auto-enable after install)
```


### `hermes.skills`

Install skills via `hermes skills install`. Skills are reconciled on every pod start.

```yaml
hermes:
  skills:                          # optional; omit if no skills are needed
    - identifier: official/finance/stocks  # required; skill path or HTTP(S) URL to a SKILL.md
      category: finance            # optional; category folder to install into
      name: stocks                 # optional; overrides the skill name from SKILL.md frontmatter
      force: false                 # optional; set true to install despite a blocked scan verdict
```


### `hermes.crons`

Schedule recurring prompts. Supported formats: `every Xh`, `every Xm`, or standard cron expressions.

```yaml
hermes:
  crons:                           # optional; omit if no scheduled jobs are needed
    - name: daily-standup          # required; human-friendly name and reconciliation key
      schedule: every 24h          # required; e.g. "every 2h", "30m", "0 9 * * *"
      prompt: Summarize yesterday's activity and suggest today's priorities  # optional
      deliver: slack               # optional; origin | local | telegram | discord | signal | platform:chat_id
      repeat: 1                    # optional; number of times to repeat the job
      skills:                      # optional; skills to attach to this job
        - stocks
      script: standup.sh           # optional; path under ~/.hermes/scripts/
      noAgent: false               # optional; true to run script and deliver stdout directly, skipping the LLM
      workdir: /home/hermes        # optional; absolute working directory for the job
      profile: default             # optional; Hermes profile name to run under
```


### `hermes.bundles`

Define slash-command bundles that group related skills under a single name.

```yaml
hermes:
  bundles:                         # optional; omit if no bundles are needed
    - name: finance                # required; becomes the /slash command name
      description: Finance helpers # optional; shown in /help and bundle list
      skills:                      # optional; skill names to include
        - stocks
      instruction: Use these tools for financial queries  # optional; prepended to skill content
      force: false                 # optional; set true to overwrite an existing bundle with the same name
```


### `hermes.env` / `hermes.envFrom`

Inject environment variables directly or from existing ConfigMaps and Secrets.

```yaml
hermes:
  env:                             # optional; omit if no direct env vars are needed
    - name: TZ
      value: UTC
  envFrom:                         # optional; omit if no ConfigMap/Secret injection is needed
    - secretRef:
        name: my-api-keys
    - configMapRef:
        name: my-agent-config
```


### `hermes.resources`

CPU and memory for the agent container. Defaults: limits `2 CPU / 4Gi`, requests `500m / 1Gi`.

```yaml
hermes:
  resources:                       # optional; omit to use defaults
    limits:
      cpu: "2"
      memory: 4Gi
    requests:
      cpu: 500m
      memory: 1Gi
```


### `hermes.initChownData`

Run an init container that sets `/opt/data` ownership to the hermes user (`10000:10000`). Useful when using a pre-existing PVC whose data was written by a different user.

```yaml
hermes:
  initChownData: true              # optional; defaults to false
```


### `searxng`

Optional sidecar that runs a local [SearXNG](https://github.com/searxng/searxng) instance, enabling the agent's `web_search` tool without an external API key. When enabled, the operator automatically injects `SEARXNG_URL` into the agent container and sets `web.search_backend: "searxng"` in the generated Hermes config (unless already set in `hermes.config.raw`):

```yaml
web:
  search_backend: "searxng"
```

```yaml
searxng:
  enabled: true                    # defaults to false; omit the entire block to disable
  image:                           # optional; omit to use the default image (searxng/searxng:latest)
    repository: searxng/searxng
    tag: latest
  resources:                       # optional; omit to use no resource constraints
    limits:
      cpu: 500m
      memory: 512Mi
    requests:
      cpu: 100m
      memory: 128Mi
  configFiles:                     # optional; files mounted at /etc/searxng
    settings.yml: |                # omit to use the operator default (enables JSON response format)
      use_default_settings: true
      search:
        formats:
          - html
          - json
  persistence:                     # optional; omit to use an emptyDir (state lost on restart)
    enabled: true
    size: 1Gi                      # optional; defaults to 1Gi
    storageClassName: standard     # optional; omit to use the cluster default StorageClass
    existingClaim: my-searxng-pvc  # optional; omit to provision a new PVC automatically
  extraEnv:                        # optional; additional env vars for the SearXNG container
    - name: SEARXNG_SECRET
      value: my-secret
```


### `camofox`

Optional sidecar for browser automation via [Camofox](https://github.com/jo-inc/camofox-browser). When enabled, `CAMOFOX_URL` is automatically injected into the agent container.

```yaml
camofox:
  enabled: true                    # defaults to false; omit the entire block to disable
  image:                           # optional; omit to use the default image (ghcr.io/jo-inc/camofox-browser:latest)
    repository: ghcr.io/jo-inc/camofox-browser
    tag: latest
  resources:                       # optional; omit to use no resource constraints
    limits:
      cpu: "1"
      memory: 1Gi
    requests:
      cpu: 200m
      memory: 256Mi
  persistence:                     # optional; omit to use an emptyDir (browser state lost on restart)
    enabled: true
    size: 1Gi                      # optional; defaults to 1Gi
    storageClassName: standard     # optional; omit to use the cluster default StorageClass
    existingClaim: my-camofox-pvc  # optional; omit to provision a new PVC automatically
  extraEnv:                        # optional; additional env vars for the Camofox container
    - name: DISPLAY
      value: ":99"
```


### `security`

RBAC and NetworkPolicy configuration. A ServiceAccount is created by default. NetworkPolicy is created when the block is present.

```yaml
security:
  rbac:                            # optional; omit to skip RBAC resource creation
    createServiceAccount: true     # optional; defaults to true
    serviceAccountName: my-sa      # optional; used only when createServiceAccount is false
    serviceAccountAnnotations:     # optional; use for cloud provider identity (AWS IRSA, GCP Workload Identity)
      eks.amazonaws.com/role-arn: arn:aws:iam::123456789:role/my-role
    additionalRules:               # optional; extra rules appended to the generated Role
      - apiGroups: [""]
        resources: ["secrets"]
        verbs: ["get", "list"]
  networkPolicy:                   # optional; omit the entire block to skip NetworkPolicy creation
    enabled: true                  # optional; defaults to true when the block is present
    allowedIngressCIDRs:           # optional; CIDRs allowed to reach this agent
      - 10.0.0.0/8
    allowedIngressNamespaces:      # optional; namespaces allowed to reach this agent
      - my-namespace
    allowedEgressCIDRs:            # optional; CIDRs this agent can reach (default allows 443 for AI APIs)
      - 0.0.0.0/0
    allowDNS: true                 # optional; defaults to true (allows port 53)
    additionalEgress:              # optional; custom egress rules beyond DNS + HTTPS defaults
      - ports:
          - port: 5432
            protocol: TCP
```


### `networking`

Service type and optional Ingress for exposing the agent's gateway (port `8642`).

```yaml
networking:
  service:
    type: ClusterIP                # optional; ClusterIP (default) | LoadBalancer | NodePort
    annotations:                   # optional; custom annotations on the Service
      service.beta.kubernetes.io/aws-load-balancer-type: nlb
    ports:                         # optional; omit to expose only the default gateway port (8642)
      - name: gateway              # required per port entry
        port: 8642
        targetPort: 8642           # optional; defaults to port
        protocol: TCP              # optional; TCP (default) | UDP | SCTP
  ingress:
    enabled: true                  # optional; defaults to false
    className: nginx               # optional; name of the IngressClass to use
    annotations:                   # optional; custom annotations on the Ingress
      cert-manager.io/cluster-issuer: letsencrypt
    hosts:                         # optional; omit if Ingress is not needed
      - host: agent.example.com
        paths:
          - path: /                # optional; defaults to /
            pathType: Prefix       # optional; Prefix (default) | Exact | ImplementationSpecific
            port: 8642             # optional; defaults to the gateway port (8642)
    tls:                           # optional; omit if TLS termination is not needed
      - hosts:
          - agent.example.com
        secretName: agent-tls
```


### `suspend`

Pause the agent by scaling its StatefulSet to 0 without deleting the resource or its data.

```yaml
suspend: true                      # optional; defaults to false
```


## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

Copyright 2026. Licensed under the [MIT License](./LICENSE).
