# Team installation guide

This guide covers namespace-scoped `hermes-agent-operator` installs for shared clusters.
It assumes a platform/admin team owns cluster-scoped resources such as CRDs and admission
policies, while each application team owns a namespace-scoped Helm release.

## Install model

1. Platform admin installs or upgrades the `HermesAgent` CRD once per cluster.
2. Team installs the operator in its namespace with namespaced RBAC.
3. Team creates `HermesAgent` objects and same-namespace Secrets.

```sh
# Platform/admin step: install or update cluster-scoped CRDs.
helm template hermes-agent-operator oci://ghcr.io/hermeum/charts/hermes-agent-operator \
  --version 0.3.0 \
  --set manager.enabled=false \
  --set crd.enable=true \
| kubectl apply -f -

# Team step: namespaced operator install; does not manage CRDs.
helm upgrade hermes-agent-operator oci://ghcr.io/hermeum/charts/hermes-agent-operator \
  --install --namespace team-a-hermes --create-namespace \
  --version 0.3.0 \
  -f dist/chart/values-team.yaml \
  --set manager.image.tag=0.5.0 \
  --wait --timeout 5m
```

`dist/chart/values-team.yaml` sets:

```yaml
crd:
  enable: false
rbac:
  namespaced: true
metrics:
  secure: false
```

Secure controller-runtime metrics require cluster-scoped TokenReview and
SubjectAccessReview permissions, so team installs either disable secure metrics or use a
platform-owned cluster-wide release.

## Orgo/k3s API connectivity

Some Orgo+k3s clusters cannot route from Pods to `https://kubernetes.default.svc`, even
though the node-local k3s API is reachable on `127.0.0.1:6443`. Symptoms include manager
logs with Kubernetes API connection failures and `HermesAgent` objects that never
reconcile.

Workaround: run the manager with host networking and point the in-cluster client at the
node-local API endpoint.

```sh
helm upgrade hermes-agent-operator oci://ghcr.io/hermeum/charts/hermes-agent-operator \
  --install --namespace team-a-hermes --create-namespace \
  --version 0.3.0 \
  -f dist/chart/values-team.yaml \
  --set manager.image.tag=0.5.0 \
  --set manager.hostNetwork=true \
  --set manager.dnsPolicy=ClusterFirstWithHostNet \
  --set manager.env[0].name=KUBERNETES_SERVICE_HOST \
  --set-string manager.env[0].value=127.0.0.1 \
  --set manager.env[1].name=KUBERNETES_SERVICE_PORT \
  --set-string manager.env[1].value=6443 \
  --wait --timeout 5m
```

Prefer fixing cluster DNS/service routing when possible. Use host networking only for the
operator manager Pod, not for agent Pods.

## Pin images and upgrades

For production/team installs, pin both the chart and images. Do not rely on `latest`.

```sh
helm upgrade hermes-agent-operator oci://ghcr.io/hermeum/charts/hermes-agent-operator \
  --install --namespace team-a-hermes \
  --version 0.3.0 \
  -f dist/chart/values-team.yaml \
  --set manager.image.tag=0.5.0
```

For stronger immutability, pin by digest:

```sh
helm upgrade hermes-agent-operator oci://ghcr.io/hermeum/charts/hermes-agent-operator \
  --install --namespace team-a-hermes \
  --version 0.3.0 \
  -f dist/chart/values-team.yaml \
  --set manager.image.digest=sha256:<controller-image-digest>
```

Recommended upgrade path:

1. Read release notes and CRD schema changes.
2. Platform admin applies CRDs first.
3. Teams upgrade namespace-scoped releases with `crd.enable=false`.
4. Verify manager rollout and one canary `HermesAgent` before broad rollout.
5. Rollbacks can safely roll back the controller image/chart templates, but do not assume
   Helm can downgrade CRD schemas. Validate CRD rollback plans against existing objects.

Agent runtime images can also be pinned per `HermesAgent`:

```yaml
apiVersion: agents.hermeum.app/v1alpha1
kind: HermesAgent
metadata:
  name: my-agent
spec:
  hermes:
    image:
      repository: nousresearch/hermes-agent
      tag: "<pinned-version>"
```

## External Secrets integration

The operator does not need direct access to Vault, cloud secret managers, or External
Secrets APIs. It reads ordinary Kubernetes Secrets referenced from the `HermesAgent` in
the same namespace. Any controller that materializes a Kubernetes Secret works, including
External Secrets Operator and Vault Secrets Operator.

Create the `ExternalSecret` before the `HermesAgent` when possible. If the target Secret
is delayed, Kubernetes may keep the agent Pod pending or restarting until the Secret key
exists.

```yaml
apiVersion: external-secrets.io/v1
kind: SecretStore
metadata:
  name: team-secret-store
  namespace: team-a-hermes
spec:
  provider:
    # Example only; configure the provider/auth for your environment.
    vault:
      server: https://vault.example.com
      path: secret
      version: v2
      auth:
        kubernetes:
          mountPath: kubernetes
          role: team-a-hermes
          serviceAccountRef:
            name: external-secrets
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: my-hermes-secret
  namespace: team-a-hermes
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: team-secret-store
    kind: SecretStore
  target:
    name: my-hermes-secret
    creationPolicy: Owner
  data:
    - secretKey: ANTHROPIC_API_KEY
      remoteRef:
        key: teams/team-a/hermes
        property: anthropic_api_key
    - secretKey: API_SERVER_KEY
      remoteRef:
        key: teams/team-a/hermes
        property: api_server_key
    - secretKey: WEBHOOK_SECRET
      remoteRef:
        key: teams/team-a/hermes
        property: webhook_secret
---
apiVersion: agents.hermeum.app/v1alpha1
kind: HermesAgent
metadata:
  name: my-agent
  namespace: team-a-hermes
spec:
  hermes:
    config:
      raw:
        model:
          provider: anthropic
          default: claude-sonnet-4-6
      apiServer:
        enabled: true
        existingSecret:
          name: my-hermes-secret
          key: API_SERVER_KEY
      webhook:
        enabled: true
        secretRef:
          name: my-hermes-secret
          key: WEBHOOK_SECRET
    envFrom:
      - secretRef:
          name: my-hermes-secret
```

Do not commit literal provider API keys to GitOps repositories. Commit only references to
external secret paths and keep access to those paths scoped to the team namespace.

## Admission guardrails

A `HermesAgent` can run code and influence Pods, volumes, Services, Ingresses,
NetworkPolicies, and optional namespaced RBAC. Treat write access to `HermesAgent` as the
ability to run code in that namespace.

For shared clusters, install admission policy that blocks unsafe specs by default and
requires an explicit review label for exceptions. A concrete Kubernetes
`ValidatingAdmissionPolicy` example lives in:

- [`docs/admission/hermesagent-team-guardrails.yaml`](./admission/hermesagent-team-guardrails.yaml)

The example blocks:

- `hostPath` volumes;
- privileged containers and `allowPrivilegeEscalation: true` in user-supplied sidecars or
  init containers;
- user-supplied `sidecars` or `initContainers` unless labeled
  `hermeum.app/allow-custom-containers: "true"`;
- `LoadBalancer`/`NodePort` Services and enabled Ingress unless labeled
  `hermeum.app/allow-public-exposure: "true"`;
- wildcard `spec.security.rbac.additionalRules`;
- externally exposed agents without an enabled `spec.security.networkPolicy`.

Apply it as a platform/admin step after the CRD exists:

```sh
kubectl apply -f docs/admission/hermesagent-team-guardrails.yaml
```

Adjust labels and allowed patterns to match your cluster's approval workflow.
