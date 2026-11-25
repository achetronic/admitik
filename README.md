# Admitik

**Cloud native policy engine for Kubernetes**

![GitHub go.mod Go version (subdirectory of monorepo)](https://img.shields.io/github/go-mod/go-version/admitikcore/admitik)
![GitHub](https://img.shields.io/github/license/admitikcore/admitik)

![YouTube Channel Subscribers](https://img.shields.io/youtube/channel/subscribers/UCeSb3yfsPNNVr13YsYNvCAw?label=achetronic&link=http%3A%2F%2Fyoutube.com%2Fachetronic)
![GitHub followers](https://img.shields.io/github/followers/achetronic?label=achetronic&link=http%3A%2F%2Fgithub.com%2Fachetronic)
![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/achetronic?style=flat&logo=twitter&link=https%3A%2F%2Ftwitter.com%2Fachetronic)

<img src="https://raw.githubusercontent.com/admitikcore/admitik/master/docs/img/header.png" alt="Admitik Header (Main) logo." width="100%">

> [!NOTE]
> **Where this project comes from**
>
> Admitik was originally created at Magnific (formerly Freepik) by the maintainers of this
> repository. When we left the company we lost access to the repository we had been working on, so
> the same team is continuing the project here as a spin-off. Same engine, same ideas, and the
> intention to keep improving it in the open for everyone.

Admitik is a cloud native policy engine for Kubernetes that lets you define policies to
**validate**, **mutate**, **generate**, **clone**, or **clean** resources.

It uses template engines (CEL, Starlark, Go templates) to apply logic, patch resources, or generate
new ones, all directly inside your cluster.

No new languages to learn. No sidecars. No external database. Just Kubernetes-native power. 💪

## ✨ What can Admitik do?

#### ✅ **Validation**
Enforce admission rules to keep your cluster secure, compliant, and predictable.

- Block configurations that violate security or runtime policies
- Enforce consistent naming, labeling, or structural patterns
- Reject resources that miss required platform standards (e.g. limits, roles, labels)

#### 🔁 **Mutation**
Modify resources before they’re stored to ensure they meet platform expectations.

- Auto-inject metadata for cost tracking, ownership, or auditing
- Add observability settings (e.g. monitoring annotations) automatically
- Apply missing defaults for scheduling, networking, or access behavior

#### 📦 **Generation**
Create complementary resources in response to cluster activity.

- Deploy baseline policies or controls when new environments appear
- Automatically provision RBAC or access scopes based on context
- Generate environment-specific configs to simplify onboarding

#### 🧬 **Cloning**

> [!IMPORTANT]
> We are working on this feature! 🛠️

#### 🧹 **Cleanup**

> [!IMPORTANT]
> We are working on this feature! 🛠️

Today the only thing that goes away on its own is what a generation policy wrote, when it is
removed with `deletionPolicy: Delete`.

## 🧰 Template Engines

Admitik uses templating to evaluate conditions, build messages, craft patches, or define generated
objects. Each template can pick its engine, and the engines can be combined in the same policy.

| Engine | What it is |
|---------------|--------------------------------------------------------------------------|
| `cel`         | Common Expression Language. The default. Read-only, for expressions. |
| `gotmpl`      | Go templates, with [Sprig functions](https://masterminds.github.io/sprig/). |
| `starlark`    | A Python-like scripting language. Loops, state, and `vars` you can write. |
| `plain`       | No templating: you write the value. |
| `plain+cel`   | A plain string with inline `{{ }}` CEL expressions. |

### 🧠 Evaluation Context

Inside any template, you can access these variables:

| Key         | Description                                                                                         |
|-------------|-----------------------------------------------------------------------------------------------------|
| `object`    | The resource being created, updated, or deleted                                                     |
| `oldObject` | The previous version (on `UPDATE` operations)                                                       |
| `operation` | The current action: `CREATE`, `UPDATE`, or `DELETE`                                                 |
| `sources`   | Lists of extra Kubernetes resources you request for evaluation (like `ConfigMaps` or `Deployments`) |
| `vars`      | A shared dictionary to store and reuse values across conditions and templates                       |

These variables let you write dynamic, context-aware policies using real cluster data. 🔍

> [!TIP]
> All the variables are available to every engine, but not all engines can do everything with them.
> CEL is for expressions, so it can read `vars` but cannot modify them. Starlark can.

## 📂 Policy Kinds

| Kind                      | What it does                                         |
|---------------------------|------------------------------------------------------|
| `ClusterValidationPolicy` | Validates intercepted resources                      |
| `ClusterMutationPolicy`   | Modifies intercepted resources                       |
| `ClusterGenerationPolicy` | Generates new resources (or clones existing) on events |

`ClusterCleanupPolicy` is not here yet. Until it lands, deleting a generation policy is how you
remove what it wrote, with `deletionPolicy: Delete`.

## 🧪 Examples

Each use case in the docs carries the policy, the explanation, and the reasoning behind it:

- [Deny privileged containers](https://admitik.dev/use-cases/deny-privileged-pods/)
- [Require an owner label on every namespace](https://admitik.dev/use-cases/require-team-label/)
- [Reject HTTPRoutes that collide with an existing one](https://admitik.dev/use-cases/avoid-colliding-httproutes/)
- [Cap the replicas of a Deployment with a namespace label](https://admitik.dev/use-cases/cap-replicas-from-namespace/)
- [Generate a ConfigMap in every new namespace](https://admitik.dev/use-cases/generate-configmap-per-namespace/)
- [Ship container logs to FluentD without touching the app](https://admitik.dev/use-cases/centralized-logging-with-fluentbit/)

👉 The rest are in the [use case catalogue](https://admitik.dev/use-cases/).

## 📚 Documentation

The full documentation lives at **[admitik.dev](https://admitik.dev)**:

| Page | What you find |
|--------------------------------------------|----------------------------------------------------------------|
| [Installation](https://admitik.dev/docs/installation/) | Helm chart, bundle, and how to remove them. |
| [Concepts](https://admitik.dev/docs/concepts/) | What a policy is made of: sources, conditions, engines, triggers. |
| [Policies](https://admitik.dev/docs/policies/) | Validation, mutation, generation, clone and cleanup. |
| [Configuration](https://admitik.dev/docs/configuration/) | Flags and their defaults. |
| [Observability](https://admitik.dev/docs/observability/) | Metrics and the log contract. |

## 📦 Installation

```console
helm install admitik oci://ghcr.io/admitikcore/admitik/charts/admitik \
  --version 1.11.1 --namespace admitik --create-namespace
```

There is also a plain `install.yaml` attached to every release, for the people who do not use Helm.
The [installation page](https://admitik.dev/docs/installation/) covers both, the certificate and the
uninstall.

## 🤝 Contributing

All contributions are welcome! Whether you're reporting bugs, suggesting features, or submitting
code, thank you! Here’s how to get involved:

▸ [Open an issue](https://github.com/admitikcore/admitik/issues/new) to report bugs or request features

▸ [Submit a pull request](https://github.com/admitikcore/admitik/pulls) to contribute improvements

▸ [Check open milestones](https://github.com/admitikcore/admitik/milestones) to see what’s coming

Development setup and the release process are in [CONTRIBUTING.md](./docs/CONTRIBUTING.md).

## 📄 License

Admitik is licensed under the [Apache 2.0 License](./LICENSE).
