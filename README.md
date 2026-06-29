# hermes-agent-operator

<p align="center"><img alt="Hermes Gopher" src="./img/hermes-agent-gopher.png" width="300" height="300"/></p>

Self-hosting [Hermes agent](https://github.com/nousresearch/hermes-agent) on Kubernetes in a declarative, reproducible manner. 

## Why

Hermes agent is a powerful tool for automating tasks — but it is designed for personal use. Running it across a team is difficult: configurations drift, skills go out of sync, and there is no shared source of truth for what each agent does or what it has access to.

`hermes-agent-operator` solves this by managing Hermes agents as Kubernetes custom resources. You declare the full state of an agent — its config, workspace files, skills, crons, and bundles — in a single manifest. The operator keeps the running agent in sync with that declaration. 

## Quick Start

**Prerequisites:** a Kubernetes cluster and Helm v3.

### 1. Install the operator

```sh
helm upgrade hermes-agent-operator oci://ghcr.io/hermeum/charts/hermes-agent-operator \
  --install --namespace hermes-agent --create-namespace
```

### 2. Create a secret with your API key

```sh
kubectl create secret generic my-hermes-secret \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-...
```

### 3. Deploy a HermesAgent

```sh
kubectl apply -f - <<EOF
apiVersion: agents.hermeum.app/v1alpha1
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

## Create a HermesAgent with the Skill

The `hermes-agent-operator` skill lets a running Hermes agent scaffold and apply `HermesAgent` manifests on your behalf. Install it once into your Hermes agent:

```sh
hermes skills install hermeum/hermes-agent-operator/skills/hermes-agent-operator
```

Then run the `/hermes-agent-operator` skill to create a custom resource.

## Configuration

- [`hermes.config`](#hermesconfig)
- [`hermes.storage`](#hermesstorage)
- [`hermes.workspace`](#hermesworkspace)
- [`hermes.packages`](#hermespackages)
- [`hermes.plugins`](#hermesplugins)
- [`hermes.skills`](#hermesskills)
- [`hermes.crons`](#hermescrons)
- [`hermes.bundles`](#hermesbundles)
- [`hermes.env` / `hermes.envFrom`](#hermesenv--hermesenvfrom)
- [`hermes.resources`](#hermesresources)
- [`hermes.initChownData`](#hermesinit​chowndata)
- [`hermes.initScripts`](#hermesinitscripts)
- [`hermes.profiles`](#hermesprofiles)
- [`searxng`](#searxng)
- [`camofox`](#camofox)
- [`security.rbac`](#securityrbac)
- [`security.networkPolicy`](#securitynetworkpolicy)
- [`networking.service`](#networkingservice)
- [`networking.ingress`](#networkingingress)
- [`suspend`](#suspend)

### `hermes.config`

Configure the Hermes agent runtime. `raw`, `apiServer`, and `webhook` can be used independently or together.

#### `raw`

Pass a verbatim `config.yml` as free-form YAML. Anything valid in a Hermes config file is accepted here.

```yaml
hermes:
  config:
    raw:                           # optional; omit if no custom config.yml is needed
      model:
        provider: anthropic
        default: claude-sonnet-4-6
```

#### `apiServer`

Enable the built-in gateway API. The operator always generates a Kubernetes Secret named `<agent-name>-hermes` containing a random `API_SERVER_KEY`. When `enabled: true`, the operator sets `API_SERVER_ENABLED=true`, `API_SERVER_PORT`, and injects the key into the agent container automatically.

```yaml
hermes:
  config:
    apiServer:                     # optional; omit to disable the gateway API
      enabled: true
      port: 8642                   # optional; defaults to 8642. 
      corsOrigins:                 # optional; browser origins allowed to call the API server
        - https://app.example.com  # (sets API_SERVER_CORS_ORIGINS). CORS stays disabled when empty
      existingSecret:              # optional; omit to use the operator-generated key
        name: my-api-key-secret    # name of the Secret in the same namespace
        key: API_SERVER_KEY        # key within that Secret
```

#### `webhook`

Enable the webhook ingress. When `enabled: true`, the operator sets `WEBHOOK_ENABLED=true` and injects a `WEBHOOK_SECRET` (the HMAC secret) into the agent container. By default the secret is generated once and stored in the operator-managed `<agent-name>-hermes` Secret, then preserved across reconciles so it is not rotated.

```yaml
hermes:
  config:
    webhook:                       # optional; omit to disable the webhook ingress
      enabled: true
      port: 8644                   # optional; defaults to 8644. 
      secretRef:                   # optional; omit to use the operator-generated secret
        name: my-webhook-secret    # name of the Secret in the same namespace
        key: WEBHOOK_SECRET        # key within that Secret
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

Seed files and secrets into the agent's home directory before startup.

#### `files`

Seed files into the agent's home directory. Keys are relative paths; use `/` as a separator for subdirectories.

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

#### `dotEnv`

Generate a `$HERMES_HOME/.env` file from a Kubernetes Secret. Each key in the Secret becomes a `KEY=VALUE` line in the file. Useful for tools that read `.env` files at startup.

```yaml
hermes:
  workspace:
    dotEnv:                        # optional; omit if no .env file is needed
      secretRef:
        name: my-env-secret        # name of the Secret in the same namespace
```


### `hermes.packages`

Pre-install language packages before the agent starts. Each sub-key corresponds to a package manager (`pip`, `npm`). Packages are installed via init containers and stored on the persistent volume, so they survive pod restarts.

Removing a package from any list wipes and reinstalls the remaining set on the next reconcile, so the installed state always matches the declaration.

#### `pip`

Installs Python packages via `uv pip install`. `install` entries are [pip specifiers](https://pip.pypa.io/en/stable/reference/requirement-specifiers/) — bare name, version-pinned, or extras. `extraArgs` are appended verbatim to the `uv pip install` command, useful for custom index URLs or other flags.

Packages are installed into `$HERMES_HOME/.python-packages` and made available via `PYTHONPATH`.

```yaml
hermes:
  packages:
    pip:                           # optional
      install:                     # pip specifiers to install
        - requests
        - pandas==2.1.0
        - "beautifulsoup4[lxml]"
      extraArgs:                   # optional; extra flags passed to `uv pip install`
        - "--index-url=https://my-private-index.example.com/simple"
        - "--extra-index-url=https://pypi.org/simple"
```

Installed binaries land in `$HERMES_HOME/.python-packages/bin`. To add them to `PATH` for shell-based tools, seed a `.bashrc` via `hermes.workspace`:

```yaml
hermes:
  packages:
    pip:
      install:
        - requests
  workspace:
    files:
      home/.bashrc: |
        export PATH="$HERMES_HOME/.python-packages/bin:$PATH"
```

> **Note:** These packages are available to Python code run *by* the agent (tool execution, scripts, etc.). They do not affect the Hermes agent process itself, which uses its own virtual environment at `/opt/hermes/.venv`.

#### `npm`

Installs npm packages via `npm install -g --prefix`. `install` entries are standard npm package specifiers — bare name, scoped (`@scope/name`), or version-pinned (`pkg@^1.0.0`).

Packages are installed into `$HERMES_HOME/.npm-packages` and made available via `NODE_PATH`.

```yaml
hermes:
  packages:
    npm:                           # optional
      install:                     # npm package specifiers to install
        - "@anthropic-ai/sdk"
        - "@anthropic-ai/mcp-server-puppeteer"
        - "typescript@^5.0.0"
```

Installed binaries land in `$HERMES_HOME/.npm-packages/bin`. To add them to `PATH` for shell-based tools, seed a `.bashrc` via `hermes.workspace`:

```yaml
hermes:
  packages:
    npm:
      install:
        - typescript
  workspace:
    files:
      home/.bashrc: |
        export PATH="$HERMES_HOME/.npm-packages/bin:$PATH"
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

Run an init container that sets `/opt/data` ownership to the hermes user (`10000:10000`). Useful when using an existing PVC whose data was written by a different user.

> **Note:** Because the container starts as root (see [FAQ](#faq)), running the `hermes` command inside the container will also change the ownership of files under `/opt/data` to the hermes user (`10000:10000`).

```yaml
hermes:
  initChownData: true              # optional; defaults to false
```


### `hermes.initScripts`

Run shell scripts in init containers before the agent starts. The operator automatically uses the hermes-agent image and wires up the environment variables (`HERMES_HOME`, `HOME`, and any `hermes.env` / `hermes.envFrom`), volume mounts, and security context.

Scripts run after all operator-managed init containers. 

```yaml
hermes:
  initScripts:                       # optional; omit if no custom init scripts are needed
    - name: install-gh               # required; must be unique within the pod
      script: |                      # required; executed via /bin/sh -ec
        mkdir -p $HERMES_HOME/.local/bin
        GH_VERSION=2.94.0
        curl -fsSL "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_amd64.tar.gz" \
          | tar -xz -C /tmp
        mv "/tmp/gh_${GH_VERSION}_linux_amd64/bin/gh" $HERMES_HOME/.local/bin/gh
        chmod +x $HERMES_HOME/.local/bin/gh
```

Use `spec.initContainers` instead when you need a different image, custom volume mounts, or fine-grained resource limits on the init step.


### `hermes.profiles`

Declare named Hermes profiles that are created and configured alongside the default profile. Each profile supports its own config, workspace files, .env, plugins, skills, bundles, and crons. All profiles share a single multiplexed gateway.

```yaml
hermes:
  profiles:
    coder:                             # profile name; key becomes the profile identifier
      clone: true                      # optional; clone config/.env/SOUL.md/skills from default at creation
      config:
        raw:                           # optional; raw config.yaml content (JSON-serialized)
          model: claude-sonnet-4-5
          # NOTE: apiServer and webhook are not supported here
      workspace:
        dotEnv:                        # optional; write .env from a Secret
          secretRef:
            name: coder-env-secret
        files:                         # optional; workspace files copied to the profile home dir
          SOUL.md: |
            You are a senior software engineer...
      plugins:                         # optional; same schema as hermes.plugins
        - identifier: owner/hermes-plugin-github
      skills:                          # optional; same schema as hermes.skills
        - identifier: https://example.com/skills/code-review.md
          name: code-review
      bundles:                         # optional; same schema as hermes.bundles
        - name: engineering
          skills: [code-review]
      crons:                           # optional; same schema as hermes.crons
        - name: daily-standup
          schedule: "0 9 * * 1-5"
          prompt: "Summarise yesterday's commits"
```

The operator appends these init containers **after** `init-crons` and before any user `initScripts`:

| Container | When added |
|-----------|-----------|
| `init-profiles` | Always when profiles > 0 |
| `init-profiles-config` | Only if at least one profile has `config.raw` |
| `init-profiles-workspace` | Always when profiles > 0 |
| `init-profiles-dotenv` | Only if at least one profile has `workspace.dotEnv` |
| `init-profiles-plugins` | Always when profiles > 0 |
| `init-profiles-skills` | Always when profiles > 0 |
| `init-profiles-bundles` | Always when profiles > 0 |
| `init-profiles-crons` | Always when profiles > 0 |


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
  env:                             # optional; additional env vars for the SearXNG container
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
  env:                             # optional; additional env vars for the Camofox container
    - name: DISPLAY
      value: ":99"
```


### `security.rbac`

ServiceAccount and Role configuration. A ServiceAccount is created by default.

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
```


### `security.networkPolicy`

NetworkPolicy configuration. Created only when this block is present.

```yaml
security:
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


### `networking.service`

Service configuration for the agent. Ports defined by `hermes.config.apiServer` and `hermes.config.webhook` are automatically exposed — use `ports` only for additional ports beyond those.

```yaml
networking:
  service:
    type: ClusterIP                # optional; ClusterIP (default) | LoadBalancer | NodePort
    annotations:                   # optional; custom annotations on the Service
      service.beta.kubernetes.io/aws-load-balancer-type: nlb
    ports:                         # optional; additional ports.
      - name: metrics              
        port: 9090
        targetPort: 9090           # optional; defaults to port
        protocol: TCP              # optional; TCP (default) | UDP | SCTP
```


### `networking.ingress`

Optional Ingress for exposing the agent externally.

```yaml
networking:
  ingress:
    enabled: true                  # optional; defaults to false
    className: nginx               # optional; name of the IngressClass to use
    annotations:                   # optional; custom annotations on the Ingress
      cert-manager.io/cluster-issuer: letsencrypt
    hosts:                         # optional; omit if Ingress is not needed
      - host: agent.example.com
        paths:                     # required; at least one path per host
          - path: /                # optional; defaults to /
            pathType: Prefix       # optional; Prefix (default) | Exact | ImplementationSpecific
            port: 8642             # required; backend Service port, e.g. the API server port
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


## FAQ

**Q: How are things self-installed by Hermes managed via the custom resource?**

They aren't. The operator only manages what is explicitly declared in the `HermesAgent` custom resource. Anything Hermes installs on its own at runtime (plugins, packages, etc.) is outside the operator's control and will not be reconciled.

**Q: How do I make binaries, packages, etc. persistent?**

Only the `HERMES_HOME` path (`/opt/data`) is persisted across pod restarts. Anything that needs to survive a restart must be placed under `HERMES_HOME`. The operator sets `HOME=/opt/data/home` so tools that respect `$HOME` will write there automatically.

**Q: Why does the Hermes container run as root?**

The official Hermes image uses [s6-overlay](https://github.com/just-containers/s6-overlay), which requires the process to start as root for service supervision setup. Once initialisation is complete, s6-overlay drops privileges and runs the agent as the `hermes` user (`10000:10000`).


## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

Copyright 2026. Licensed under the [MIT License](./LICENSE).
