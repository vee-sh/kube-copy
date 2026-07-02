# kubectl-copy

A kubectl/oc plugin that intelligently copies Kubernetes resources across namespaces and clusters.

Handles the tedious parts automatically: stripping server-set metadata, resetting hardcoded
ClusterIPs and NodePorts, removing PV bindings, and detecting conflicts before they happen.

## Installation

### Via Homebrew

```bash
brew install vee-sh/tap/kube-copy
```

### From source

```bash
git clone <repo-url>
cd kube-copy
make install
```

### Via krew

```bash
kubectl krew install copy
```

## Usage

```
kubectl copy <resource>/<name> [flags]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--to-namespace` | `--to-ns` | Target namespace (defaults to source namespace) |
| `--to-name` | | New resource name (required for same-namespace copy) |
| `--to-context` | | Target kubeconfig context (for cross-cluster copy) |
| `--to-kubeconfig` | | Target kubeconfig file (for cross-cluster copy) |
| `--to-dir` | | Export resources to YAML files in this directory (one file per resource) |
| `--to-file` | | Export all resources as a single multi-doc YAML file |
| `--recursive` | `-r` | Copy the full dependency graph |
| `--dry-run` | | Preview what would be copied without making changes |
| `--yes` | `-y` | Skip confirmation prompt |
| `--quiet` | `-q` | Suppress progress output |
| `--on-conflict` | | Conflict strategy: `skip` (default), `warn`, `overwrite` |
| `--output` | `-o` | Output format for dry-run and post-apply summaries: `table` (default), `yaml`, `json` |
| `--namespace` | `-n` | Source namespace |
| `--context` | | Source kubeconfig context |
| `--kubeconfig` | | Path to kubeconfig file |

### Examples

Copy a Deployment to another namespace:

```bash
kubectl copy deployment/myapp --to-namespace staging
```

Copy with a new name in the same namespace:

```bash
kubectl copy deployment/myapp --to-name myapp-v2
```

Copy to another cluster:

```bash
kubectl copy deployment/myapp --to-context prod-cluster --to-namespace default
```

Recursive copy (also copies related ConfigMaps, Secrets, Services, Ingresses, HPAs):

```bash
kubectl copy deployment/myapp --to-namespace staging -r
```

Dry-run to see what would happen:

```bash
kubectl copy deployment/myapp --to-namespace staging -r --dry-run
```

Dry-run with YAML output (useful for piping to `kubectl apply`):

```bash
kubectl copy deployment/myapp --to-namespace staging -r --dry-run -o yaml
```

Overwrite existing resources in the target:

```bash
kubectl copy deployment/myapp --to-namespace staging --on-conflict overwrite
```

Export a resource and its dependencies to YAML files on disk:

```bash
kubectl copy deployment/myapp -r --to-dir ./manifests
```

Export everything as a single multi-doc YAML (handy for `kubectl apply -f`):

```bash
kubectl copy deployment/myapp -r --to-file ./bundle.yaml
```

## Export to Filesystem

Instead of writing to a live cluster, you can dump the (sanitized) resource and
its full dependency graph to disk. This is useful for GitOps workflows, backups,
and reviewing or templating manifests before applying them elsewhere.

Two destinations are supported:

- `--to-dir <path>`: one file per resource, named `<kind>-<name>.yaml`, placed
  in the given directory (created if missing).
- `--to-file <path>`: a single multi-document YAML file with `---` separators
  between resources.

Behavior in export mode:

- Sanitization still runs, so server-set metadata, status, and conflict-prone
  fields (ClusterIPs, NodePorts, PV bindings, etc.) are stripped just like in a
  live copy.
- `--to-namespace` and `--to-name` still apply -- they rewrite the metadata in
  the dumped files so you can target a different namespace at apply time.
- `--recursive` works as usual and includes the discovered dependency graph.
- The source cluster is still contacted to fetch resources; only the target
  cluster is skipped, so target-side conflict checks do not run.
- `--to-dir`/`--to-file` cannot be combined with `--to-context`,
  `--to-kubeconfig`, or `--dry-run`.

Example output layout with `--to-dir`:

```
manifests/
  deployment-myapp.yaml
  configmap-myapp-config.yaml
  secret-myapp-creds.yaml
  service-myapp.yaml
```

## What Gets Sanitized

Every copied resource goes through a sanitization pipeline that strips fields
which would cause conflicts or errors when creating a copy.

### Universal (all resources)

- `metadata.uid`, `resourceVersion`, `creationTimestamp`, `generation`, `selfLink`, `managedFields`
- `metadata.ownerReferences`
- `status` (entire block)
- `kubectl.kubernetes.io/last-applied-configuration` annotation

### Resource-specific

| Resource | Sanitization |
|----------|-------------|
| **Service** | Resets `clusterIP`/`clusterIPs`, clears `nodePorts`, warns on `loadBalancerIP` |
| **Pod** | Removes `nodeName`, strips auto-injected SA token volumes |
| **PVC** | Removes `volumeName` (PV binding), strips PV-bind annotations |
| **Ingress** | Warns about hardcoded hostnames and TLS entries |
| **ServiceAccount** | Removes auto-generated token secret references |
| **Job** | Strips controller-generated labels and auto-generated selector |

## Conflict Detection

Before creating each resource, the plugin checks for:

- **Existence conflicts** -- resource already exists in target (behavior controlled by `--on-conflict`):
  - `skip` (default): leave the existing resource untouched
  - `warn`: print the conflict and still attempt create (no delete-first, unlike `overwrite`)
  - `overwrite`: delete the existing resource, then create the copy
- **Address conflicts** -- hardcoded ClusterIP, NodePort, or LoadBalancer IP
- **Reference conflicts** -- referenced ConfigMap, Secret, PVC, or ServiceAccount does not exist in target (suggests using `--recursive`)

## Recursive Mode

When `--recursive` / `-r` is specified, the plugin discovers and copies the full
dependency graph:

**Forward references** (what the resource depends on):
- ConfigMaps, Secrets referenced in volumes, `envFrom`, `env.valueFrom`
- PVCs referenced in volumes
- ServiceAccounts

**Reverse references** (what depends on the resource):
- Services whose selector matches the pod template labels
- Ingresses whose backends reference those Services
- HPAs targeting the resource

Owner-managed resources (like ReplicaSets created by Deployments) are intentionally
skipped -- controllers will recreate them automatically.

## Supported Resource Types

The plugin works with any Kubernetes resource via the dynamic client. Common types
have built-in aliases:

`deployment`/`deploy`, `statefulset`/`sts`, `daemonset`/`ds`, `replicaset`/`rs`,
`pod`/`po`, `service`/`svc`, `configmap`/`cm`, `secret`, `serviceaccount`/`sa`,
`persistentvolumeclaim`/`pvc`, `ingress`/`ing`, `job`, `cronjob`/`cj`,
`horizontalpodautoscaler`/`hpa`, `networkpolicy`/`netpol`

## Development

```bash
# Build
make build

# Run tests
make test

# Cross-compile for all platforms (produces tarballs + sha256 in dist/)
make cross-build

# Lint
make lint
```

## Releasing

Releases are automated via GitHub Actions. To cut a new release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow will:

1. Run tests and linting
2. Cross-compile binaries for linux/darwin on amd64/arm64
3. Create a GitHub Release with the tarballs attached
4. Submit a PR to `kubernetes-sigs/krew-index` via `krew-release-bot`
5. Push an updated Homebrew formula to the `vee-sh/homebrew-tap` repository

### Repository Secrets

The following secret must be set in **Settings > Secrets and variables > Actions**:

| Secret | Description |
|--------|-------------|
| `HOMEBREW_TAP_TOKEN` | GitHub personal access token (classic) with `repo` scope, granting write access to `vee-sh/homebrew-tap` |

### Setting up the krew-index

The release workflow uses
[krew-release-bot](https://github.com/rajatjindal/krew-release-bot) to
automatically open a PR against `kubernetes-sigs/krew-index` on each tagged
release. For this to work:

1. The `.krew.yaml` template in the repo root must be present (already included).
2. Enable the `krew-release-bot` GitHub App on this repository
   (see [setup instructions](https://github.com/rajatjindal/krew-release-bot#setup)).

Once the initial PR is merged into the krew-index, subsequent releases are
picked up automatically.

### Setting up the Homebrew tap repo

Create the repository `vee-sh/homebrew-tap` on GitHub (if it does not exist yet).
The workflow will automatically create and update `Formula/kube-copy.rb` on
each tagged release. Users install via:

```bash
brew install vee-sh/tap/kube-copy
```

## License

Apache 2.0
