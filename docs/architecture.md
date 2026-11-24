# Architecture

Admitik is a policy engine. It reads policies from the cluster and acts on two paths: it validates
and mutates resources while the API server is admitting them, and it reacts to what already exists
in the background.

## The two paths

```
admission (synchronous, every replica)
  API server -> webhook -> server/admission -> evaluator -> decision -> response

background (asynchronous, leader only)
  informer -> controller/background -> processors -> action -> write to the cluster
```

The admission path never writes to the cluster. The background path does, and only the leader does
it, so several replicas never fight over the same object.

## One admission request

1. `server/admission` parses the `AdmissionReview` and builds the context: the object, the previous
   object and the operation.
2. It asks `policystore` which policies apply to that `{group}/{version}/{resource}/{operation}`.
3. For every policy, `evaluator.Prepare` fills the sources of the context and checks the conditions.
4. When the conditions pass, the policy renders its patch (mutation) or its message (validation)
   with the engine it declares.
5. The handler answers the API server: a JSON patch, a denial, or a warning when something could not
   be done.

## One background event

1. `controller/background` watches the resources declared in `watchedResources` of the policies,
   using the shared `informer.Pool`.
2. The event is dispatched to the processors that observed that resource.
3. The processor evaluates the conditions with `evaluator.Prepare` and, when they pass, renders the
   object and writes it with `kubeapply`, which marks it with the policy that wrote it.

## Packages

| Package | Responsibility |
| --- | --- |
| `api/v1alpha1` | The CRDs: `ClusterValidationPolicy`, `ClusterMutationPolicy`, `ClusterGenerationPolicy`, `ClusterAuditReport` |
| `internal/server/admission` | The HTTP server Kubernetes calls. It does not decide, it translates |
| `internal/evaluator` | Sources and conditions. The only place that knows how a policy is evaluated |
| `internal/template` | The engine contract and the context handed to the templates |
| `internal/template/{cel,gotmpl,plain,starlark}` | One package per engine. Each one knows only its own syntax |
| `internal/informer` | `Informer` is one watch with its cache; `Pool` shares one per scope between consumers |
| `internal/store/simple` | A generic in-memory store of collections. It does not know what it stores |
| `internal/policystore` | The store of policies. Adds the source reporting and the policy contract on top of `store/simple` |
| `internal/telemetry` | Prometheus metrics about what the policies decide |
| `internal/controller` | Shared helpers: finalizer, status conditions, webhook configuration builder |
| `internal/controller/cluster*policy` | One reconciler per policy type: fill the store and, for admission kinds, the webhook configuration |
| `internal/controller/sources` | Keeps a watch alive for every source the policies need, and gates readiness |
| `internal/controller/background` | Watches `watchedResources` and hands the events to the processors |
| `internal/action` | What to do when the conditions pass. Today: `Generate` |
| `internal/kubeapply` | Writes an unstructured object, resolving its resource and marking the provenance |
| `internal/audit` | Periodic scan of the existing objects. Writes one `ClusterAuditReport` |
| `internal/cascade` | Deletes everything a policy wrote, looking for its labels in every resource type |
| `internal/common` | Kubernetes events, event type normalization, GVR formatting |
| `internal/globals` | The application context and the raw Kubernetes clients |
| `internal/strategicmerge` | Strategic merge patch against the OpenAPI schemas |
| `internal/certificates` | Self signed certificates for the webhook server |

## Where things are written

- **Webhook configuration** (`MutatingWebhookConfiguration`, `ValidatingWebhookConfiguration`): only
  the leader. Every replica keeps its own policy store, which is what its admission server reads.
- **Generated objects**: the leader, through `kubeapply`, with `admitik.dev/managed-by`,
  `admitik.dev/policy` and `admitik.dev/policy-kind`.
- **Kubernetes events**: only from the background path, in the namespace of the object that was
  watched. A decision in the admission path is reported with a metric and, when something goes
  wrong, a warning in the response that `kubectl` prints.

## Rules of thumb

- A policy is a projection of a CR. Nothing is stored that cannot be rebuilt from the cluster.
- The informers are the event bus. Losing an event is not a problem because every action is
  idempotent and a starting informer re-lists everything.
- Ordering matters: mutation policies are evaluated by priority and then by name, so every replica
  produces the same patches.
- The cache of a watch is the one client-go keeps. The policy store never holds watched objects.
