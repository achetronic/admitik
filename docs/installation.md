# Installation

Admitik runs inside the cluster it guards. It needs no sidecars and no external database: it
watches the policies and writes the webhook configurations that Kubernetes calls before storing an
object.

## With the Helm chart

The chart is published as an OCI artifact in the packages of this repository:

```console
helm install admitik oci://ghcr.io/admitikcore/admitik/charts/admitik --version 1.11.0 --namespace admitik --create-namespace
```

It creates a Service in front of the webhooks server, generates the certificate the API server needs
to trust it, and configures the controller to call itself through that Service. Two replicas are
deployed by default: only the leader writes the webhook configurations, and all of them answer
admission requests.

## With the bundle

Every release also ships an `install.yaml` with the CRDs, the RBAC and the Deployment, for the
people who do not use Helm:

```console
kubectl apply -f https://github.com/admitikcore/admitik/releases/download/v1.11.0/install.yaml
```

It installs into `admitik` and generates its own certificate.

## Removing it

The webhook configurations are written by the controller, not by the manifests, so they have to go
before the controller does. If they stay, and the failure policy is `Fail`, the cluster keeps calling
a Service that no longer exists and rejects the objects of every intercepted type.

With Helm, the chart does it for you: a job runs after the uninstall and deletes them. Set
`controller.cleanupWebhooksOnUninstall` to `false` if you would rather do it yourself.

With the bundle, delete the two configurations first and the manifest after:

```console
kubectl delete validatingwebhookconfiguration admitik-cluster-validation-policy --ignore-not-found
kubectl delete mutatingwebhookconfiguration admitik-cluster-mutation-policy --ignore-not-found
kubectl delete -f install.yaml
```

Doing it the other way around leaves a window: the controller is still alive while the Deployment is
being removed, and it writes the configurations again on its last reconcile.

## Upgrading

The CRDs live under `crds/` in the chart and in the bundle, and `helm upgrade` does not touch them.
Apply them before upgrading, so the policies can use the fields of the new version:

```console
kubectl apply -f charts/admitik/crds/
```

## Checking an installation

`make verify-bundle` installs the bundle that is built from the sources into the cluster of your
kubeconfig, checks that the webhooks are reachable and that a policy is enforced, and removes it. It
needs an image the cluster can pull:

```console
IMG=ghcr.io/admitikcore/admitik:v1.11.0 make verify-bundle
```
