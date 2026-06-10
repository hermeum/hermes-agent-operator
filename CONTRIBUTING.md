# Contributing

## Prerequisites

- Go 1.24+
- Docker
- kubectl
- [Kind](https://kind.sigs.k8s.io/) (for e2e tests only)

## Generate code and manifests

After changing API types (`api/v1alpha1/`), regenerate the DeepCopy methods and CRD manifests:

```sh
make generate   # regenerate zz_generated.deepcopy.go
make manifests  # regenerate CRDs and RBAC from markers
kubebuilder edit --plugins=helm/v2-alpha
```

## Lint

```sh
make lint       # check for issues
make lint-fix   # check and auto-fix
```

Run `make lint-fix` after any code change before committing.

## Test

```sh
make test       # unit tests (no cluster required)
make test-e2e   # e2e tests against a Kind cluster
```

## Build and run locally

```sh
# Run the operator against the current kubeconfig context (installs CRDs first)
make install
make run
```

