# Hyperkube Client-Go Implementation Plan

## Context

The team is replacing HyperFleet/CLM with Hyperkube — a Kubernetes-native API framework
(`github.com/thetechnick/orlop-gcp-hcp`, living at
`experiments/platform-api/platform-api` in the `gcp-hcp` repo).

The public API types in `api/public/v1` are standard kubebuilder-style Go types
registered under group `hyperkube/v1`. Because the server is a real Kubernetes API server,
we can use `sigs.k8s.io/controller-runtime/pkg/client` for all CRUD operations — no
oapi-codegen or TypeSpec pipeline needed.

The existing `feat/hyperkube` branch (oapi-codegen approach) is preserved as a reference
and for testing against the running server. This branch (`feat/hyperkube-client-go`)
replaces that implementation.

## Architecture

```
gcp-hcp-ctl
└── pkg/
    └── hyperkube/
        └── client.go          ← NEW: scheme registration + client factory
                                   replaces types.gen.go + client.gen.go

    ├── cluster/
    │   ├── cmd.go             ← update: build controller-runtime client
    │   └── hyperkube.go       ← update: use v1.Cluster types
    └── nodepool/
        ├── cmd.go             ← update: same
        └── hyperkube.go       ← update: use v1.NodePool types

Dependencies added:
  github.com/thetechnick/orlop-gcp-hcp  (replace → local path, until published)
  sigs.k8s.io/controller-runtime
```

## Types comparison

| Field | oapi-codegen (current) | platform-api v1 (target) |
|---|---|---|
| Cluster.Metadata | `*hyperkube.ObjectMeta{Name: &s}` | `metav1.ObjectMeta{Name: s}` |
| Cluster.Spec.InfraID | `*string` | `string` |
| Cluster.Spec.Platform.Gcp.ProjectID | `*string` | `string` |
| Cluster.Status.Conditions | `*[]Condition` | `[]metav1.Condition` |
| NodePool.Spec.NodeCount | `*int32` | `*int32` |

The target types are much cleaner (fewer pointers, standard `metav1` types).

## Tasks

### Task 1 — Add dependency on the platform-api module

In `go.mod`:
```
require github.com/thetechnick/orlop-gcp-hcp v0.0.0
replace github.com/thetechnick/orlop-gcp-hcp => ../gcp-hcp/experiments/platform-api/platform-api
```

Also add `sigs.k8s.io/controller-runtime` (already used by platform-api, version ~0.24.1).

Run `go mod tidy`.

Note: The `replace` path is relative to the module root. The absolute path is
`/home/cveiga/go/src/github.com/openshift-online/gcp-hcp/experiments/platform-api/platform-api`.
Once the module is published to a proper module path this replace directive is removed.

Verification: `go build ./...`

---

### Task 2 — Create `pkg/hyperkube/client.go`

Replace `types.gen.go` and `client.gen.go` (delete them) with a single hand-written file:

```go
package hyperkube

import (
    "fmt"

    publicv1 "github.com/thetechnick/orlop-gcp-hcp/api/public/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/client-go/rest"
    "sigs.k8s.io/controller-runtime/pkg/client"
)

var scheme = runtime.NewScheme()

func init() {
    if err := publicv1.AddToScheme(scheme); err != nil {
        panic(err)
    }
}

// NewClient builds a controller-runtime typed client pointed at the hyperkube API server.
// baseURL is the server URL, e.g. "http://localhost:8081".
func NewClient(baseURL string) (client.Client, error) {
    if baseURL == "" {
        return nil, fmt.Errorf("hyperkube endpoint is required")
    }
    cfg := &rest.Config{Host: baseURL}
    return client.New(cfg, client.Options{Scheme: scheme})
}
```

Verification: `go build ./pkg/hyperkube/...`

---

### Task 3 — Update `pkg/cluster/cmd.go`

Change the `hyperkubeClientFromCmd` helper to return `(client.Client, bool)` and store a
`client.Client` in the context instead of `*hyperkube.ClientWithResponses`.

Key change in `PersistentPreRunE`:
```go
if ep, _ := cmd.Flags().GetString("hyperkube-endpoint"); ep != "" {
    hkClient, err := hyperkube.NewClient(ep)
    if err != nil {
        return err
    }
    cmd.SetContext(context.WithValue(cmd.Context(), hyperkubeClientKey, hkClient))
    return nil  // skip hyperfleet validation
}
```

Verification: `go build ./pkg/cluster/...`

---

### Task 4 — Update `pkg/cluster/hyperkube.go`

Rewrite using `client.Client` and `publicv1.Cluster` / `publicv1.ClusterList`:

- `listClustersHyperkube`: `hkClient.List(ctx, &publicv1.ClusterList{})`, no namespace
- `getClusterHyperkube`: `hkClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, &cluster)`
- `createClusterHyperkube`: build `publicv1.Cluster{...}`, call `hkClient.Create(ctx, &cluster, ...)`
- `deleteClusterHyperkube`: `hkClient.Delete(ctx, &cluster, ...)`

Helper accessors become trivial — `Cluster.Name` is `c.Name` (string, not `*string`).
Status conditions are `[]metav1.Condition` — use `meta.IsStatusConditionTrue("Ready")`.

Verification: `go build ./pkg/cluster/...`

---

### Task 5 — Update `pkg/nodepool/cmd.go` and `pkg/nodepool/hyperkube.go`

Same pattern as Tasks 3–4 but for `NodePool` / `NodePoolList`.

Verification: `go build ./pkg/nodepool/...`

---

### Task 6 — Clean up

- Delete `pkg/hyperkube/types.gen.go` and `pkg/hyperkube/client.gen.go`
- Remove `github.com/oapi-codegen/runtime` from `go.mod` (if no longer used elsewhere)
- Remove or archive `api/hyperkube/` (TypeSpec source + generated OpenAPI spec)
- Remove `generate-hyperkube-*` targets from `Makefile`
- Run `go mod tidy`

Verification: `go build ./...` and `make test`

---

## Open questions

1. **Module name**: `github.com/thetechnick/orlop-gcp-hcp` is experimental. The replace
   directive works for local development, but CI will need the path accessible. Track when
   the module moves to a stable path under `openshift-online`.

2. **Authentication**: The local server at `localhost:8081` is unauthenticated. Production
   will need a bearer token or kubeconfig. Plan: accept `--hyperkube-kubeconfig` flag or
   reuse `KUBECONFIG` env var and build `rest.Config` from it.

3. **HyperFleet cutover**: Once hyperkube is production-ready, remove the hyperfleet
   dispatch entirely. The `hyperkubeClientFromCmd` / `clientFromCmd` split in `cmd.go`
   makes this a one-line removal per command.
