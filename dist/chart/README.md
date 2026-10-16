# hermes-agent-operator Helm chart

Installs the [hermes-agent-operator](https://github.com/hermeum/hermes-agent-operator)
via Helm. The manager Deployment, RBAC, metrics service, and (by default) the
`HermesAgent` CRDs.

Install from the OCI registry:

```sh
helm upgrade hermes-agent-operator oci://ghcr.io/hermeum/charts/hermes-agent-operator \
  --install --namespace hermes-agent --create-namespace
```

## Breaking change

### Boolean values renamed `enable` -> `enabled` (v0.6.0)

Chart v0.6.0 standardises every boolean toggle on `enabled` (the prevailing
Helm convention). The following keys were renamed:

| v0.5.5 (old)              | v0.6.0 (new)                |
|---------------------------|-----------------------------|
| `rbac.helpers.enable`     | `rbac.helpers.enabled`      |
| `serviceAccount.enable`   | `serviceAccount.enabled`    |
| `crd.enable`              | `crd.enabled`               |
| `metrics.enable`          | `metrics.enabled`           |
| `certManager.enable`      | `certManager.enabled`       |
| `prometheus.enable`       | `prometheus.enabled`        |

This is a **breaking change** for any values file that sets one of the old
keys. Helm does not error on unknown keys — the old key is ignored and the
new key falls back to its default (`true` for `serviceAccount`/`crd`/
`metrics`, `false` for `rbac.helpers`/`certManager`/`prometheus`), which can
silently change what gets installed. Update yours with:

```sh
sed -i 's/\.enable:/.enabled:/g' my-values.yaml
```

(Only `enable:` keys are renamed; unrelated `enabled:` keys such as
`manager.enabled`, `manager.heartbeat.enabled`, and
`cert-manager`'s `crds.enabled` are unaffected.)

## Values

| Key | Default | Description |
|-----|---------|-------------|
| `manager.enabled` | `true` | Install the controller manager Deployment |
| `manager.replicas` | `1` | Manager replica count |
| `manager.heartbeat.enabled` | `true` | Anonymous install-count heartbeat (PostHog) |
| `rbac.namespaced` | `false` | Scope RBAC to the release namespace instead of the cluster |
| `rbac.watchNamespaces` | `[]` | Restrict the operator to listed namespaces |
| `rbac.helpers.enabled` | `false` | Install admin/editor/viewer helper roles for the CRDs |
| `serviceAccount.enabled` | `true` | Install the chart's ServiceAccount |
| `serviceAccount.name` | `""` | Use an existing ServiceAccount instead |
| `crd.enabled` | `true` | Install the `HermesAgent` CRDs with the chart |
| `crd.keep` | `true` | Keep CRDs on uninstall |
| `metrics.enabled` | `true` | Expose the controller `/metrics` endpoint |
| `metrics.port` | `8443` | Metrics server port |
| `metrics.secure` | `true` | Serve metrics over HTTPS with certs/auth |
| `certManager.enabled` | `false` | Use cert-manager for webhook/metrics certificates |
| `prometheus.enabled` | `false` | Create a ServiceMonitor for prometheus-operator |

The `ServiceMonitor` renders only when **both** `prometheus.enabled=true`
and `metrics.enabled=true`. The full surface (affinity, resources,
security contexts, labels/annotations overrides, …) is documented inline in
[`values.yaml`](./values.yaml).