# How to develop

### Prerequisites
- go version v1.24.2+, the one in `go.mod`
- docker version 17.03+
- kubectl
- Access to a Kubernetes cluster. [Kind](https://kind.sigs.k8s.io/) is enough

The tools the project needs (`controller-gen`, `kustomize`, `golangci-lint`, `setup-envtest`, `caddy`) are
downloaded into `bin/` by the Makefile the first time they are used.

### The process

> We recommend you to use a development tool like [Kind](https://kind.sigs.k8s.io/) or [Minikube](https://minikube.sigs.k8s.io/docs/start/)
> to launch a lightweight Kubernetes on your local machine for development purposes

For learning purposes, we will suppose you are going to use Kind. So the first step is to create a Kubernetes cluster
on your local machine executing the following command:

```console
kind create cluster
```

Once you have launched a safe play place, execute the following command. It will install the custom resource definitions
(CRDs) in the cluster configured in your ~/.kube/config file and run Admitik locally against the cluster:

```console
make install run
```

> [!IMPORTANT]
> `make run` refuses to start unless the current context is a `kind-` one. It starts a Caddy reverse
> proxy on the Docker bridge gateway of the cluster and points the controller to it, with the
> certificate Caddy generates, so the API server can reach the webhooks server on your machine.
> Caddy is downloaded into `bin/` by the Makefile.

If you would like to test the operator against some resources, the [use cases](https://admitik.dev/use-cases/)
carry ready-to-apply policies. Copy one of the manifests into a file and apply it to your Kind cluster:

```sh
kubectl apply -f policy.yaml
```

> Remember that your `kubectl` is pointing to your Kind cluster. However, you should always review the context your
> kubectl CLI is pointing to


## Before sending a change

```console
make test          # unit tests with the race detector
make lint          # the linter
make verify-chart  # the generated code, the CRDs and the chart are in sync
```

If you touch anything under `api/`, regenerate the CRDs and the deepcopy code, and copy the CRDs
into the chart:

```console
make manifests generate chart-crds
```

`make manifests` writes the CRDs, the RBAC and the webhook manifests. `make generate` writes
`zz_generated.deepcopy.go`. `make chart-crds` copies the CRDs into the chart. Running them twice
must produce no changes: `make verify-chart` fails when they do.

The pull request runs the same checks, plus the installer bundle on a Kind cluster: it builds
`dist/install.yaml`, installs it, checks that a policy is enforced and that the cluster is usable
after removing it. To run it locally, point `IMG` at an image the cluster can pull:

```console
IMG=admitik:dev make docker-build
kind load docker-image admitik:dev
IMG=admitik:dev make verify-bundle
```

If you touch a log line, the fields are a contract: see the [logs reference](https://admitik.dev/docs/observability/logs/).

## How releases are created

A release is a tag and a published GitHub release. Publishing it runs three pipelines, each one
with a guard that stops it when the artifacts do not agree with the tag:

| Pipeline | What it publishes |
|-------------------------------|--------------------------------------------------------------------|
| `Build Docker image` | `ghcr.io/admitikcore/admitik:vX.Y.Z` |
| `Bundle Kubernetes manifests` | `install.yaml`, attached to the release |
| `Release Charts` | `oci://ghcr.io/admitikcore/admitik/charts` |

### Publishing a release

1. Bump `version` and `appVersion` in `charts/admitik/Chart.yaml`, in a pull request, and merge it.
   `Release Charts` refuses to publish a chart whose version is not the tag.

2. Create the tag `vX.Y.Z` on that merge and publish a GitHub release for it. The tag is what the
   pipelines check out; the release is what creates it.

3. Watch the three pipelines. They run on their own when the release is published.

To retry one of them, run it by hand with `workflow_dispatch` and pass the `version`. Set
`from_master` when the commit you need to publish is only on master.
