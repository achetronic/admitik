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

Admitik is a cloud native policy engine for Kubernetes. Policies are custom resources inside the
cluster: Admitik watches them and answers the API server before an object is stored, or reacts to
what already exists and writes the objects a policy asks for.

- **Validate** what reaches the cluster and reject what breaks the rules.
- **Mutate** objects before they are stored: labels, annotations, patches, sidecars.
- **Generate** new resources on cluster events, or clone the ones that already exist.
- **Clean up** what is no longer needed.
- **Template** everything with CEL, Go templates, Starlark or plain strings.

No new languages to learn. No sidecars. No external database.

## 📚 Documentation

The full documentation lives at **[admitik.dev](https://admitik.dev)**:

| Page | What you find |
|--------------------------------------------|----------------------------------------------------------------|
| [Installation](https://admitik.dev/docs/installation/) | Helm chart, bundle, and how to remove them. |
| [Concepts](https://admitik.dev/docs/concepts/) | What a policy is made of: sources, conditions, engines, triggers. |
| [Policies](https://admitik.dev/docs/policies/) | Validation, mutation, generation, clone and cleanup. |
| [Configuration](https://admitik.dev/docs/configuration/) | Flags and their defaults. |
| [Observability](https://admitik.dev/docs/observability/) | Metrics and the log contract. |
| [Use cases](https://admitik.dev/use-cases/) | Working policies to copy and adapt. |

### Quick start

```console
helm install admitik oci://ghcr.io/admitikcore/admitik/charts/admitik \
  --version 1.11.1 --namespace admitik --create-namespace
```

The [installation page](https://admitik.dev/docs/installation/) covers the bundle, the certificate
and the uninstall.

## 🤝 Contributing

All contributions are welcome! Whether you're reporting bugs, suggesting features, or submitting code — thank you! Here’s how to get involved:

▸ [Open an issue](https://github.com/admitikcore/admitik/issues/new) to report bugs or request features

▸ [Submit a pull request](https://github.com/admitikcore/admitik/pulls) to contribute improvements

▸ [Check open milestones](https://github.com/admitikcore/admitik/milestones) to see what’s coming

Development setup and the release process are in [CONTRIBUTING.md](./docs/CONTRIBUTING.md).

## 📄 License

Admitik is licensed under the [Apache 2.0 License](./LICENSE).
