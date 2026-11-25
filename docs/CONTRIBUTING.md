# How to develop

### Prerequisites
- go version v1.24.2+, the one in `go.mod`
- docker version 17.03+
- kubectl
- Access to a Kubernetes cluster. [Kind](https://kind.sigs.k8s.io/) is enough

The tools the project needs (`controller-gen`, `kustomize`, `golangci-lint`, `setup-envtest`) are
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
(CRDs) in the cluster configured in your ~/.kube/config file and run Kuberbac locally against the cluster:

```console
make install run
```

> [!IMPORTANT]
> When executing this, a temporary public reverse tunnel will be created.
> It goes from Kube Apiserver to your local webhooks server. It's done this way to be able to test the webhooks server
> using local development tools such as **Kind**

If you would like to test the operator against some resources, the [use cases](https://admitik.dev/use-cases/)
carry ready-to-apply policies. Copy the manifest of one of them and apply it to your Kind cluster:

```sh
kubectl apply -f policy.yaml
```

> Remember that your `kubectl` is pointing to your Kind cluster. However, you should always review the context your
> kubectl CLI is pointing to


## Before sending a change

```console
make test    # runs the unit tests with the race detector
make lint    # runs the linter
```

If you touch anything under `api/`, regenerate the CRDs and the deepcopy code, and copy the CRDs
into the chart:

```console
make manifests generate chart-crds
```

`make manifests` writes the CRDs, the RBAC and the webhook manifests. `make generate` writes
`zz_generated.deepcopy.go`. `make chart-crds` copies the CRDs into the chart. Running them twice
must produce no changes: `make verify-chart` fails when they do, and the checks of the pull
request run it.

If you touch a log line, the fields are a contract: see the [logs reference](https://admitik.dev/docs/observability/logs/).

## How releases are created

Each release of this operator is done following several steps carefully in order not to break the things for anyone.
Reliability is important to us, so we automated all the process of launching a release. For a better understanding of
the process, the steps are described in the following recipe:

1. Test the changes on the code:

    ```console
    make test
    ```

   > A release is not done if this stage fails


2. Define the package information

    ```console
    export VERSION="0.0.1"
    export IMG="ghcr.io/admitikcore/admitik:v$VERSION"
    ```

3. Generate and push the Docker image (published on Docker Hub).

    ```console
    make docker-build docker-push
    ```

4. Generate the manifests for deployments using Kustomize

   ```console
    make build-installer
    ```
