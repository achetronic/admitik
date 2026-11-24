# Configuration

## Flags

Some configuration parameters can be defined by flags that can be passed to the controller.
They are described in the following table:

| Name                                   | Description                                                                           |        Default         |
|:---------------------------------------|:--------------------------------------------------------------------------------------|:----------------------:|
| `--metrics-bind-address`               | The address the metric endpoint binds to. `0` disables the server                      |          `0`           |
| `--health-probe-bind-address`          | The address the probe endpoint binds to                                               |        `:8081`         |
| `--leader-elect`                       | Enable leader election for controller manager                                         |        `false`         |
| `--metrics-secure`                     | If set the metrics endpoint is served securely                                        |        `false`         |
| `--enable-http2`                       | If set, HTTP/2 will be enabled for the metrics and webhook servers                    |        `false`         |
| `--sources-time-to-resync-informers`   | Interval to resynchronize all resources in the informers                              |         `60s`          |
| `--webhook-client-hostname`            | The hostname used by Kubernetes when calling the webhooks server                       | `webhooks.admitik.svc` |
| `--webhook-client-port`                | The port used by Kubernetes when calling the webhooks server                          |        `10250`         |
| `--webhook-client-timeout`             | The seconds Kubernetes waits before considering the webhook call a timeout            |          `10`          |
| `--webhook-server-port`                | The port where the webhooks server listens                                            |        `10250`         |
| `--webhook-server-path`                | The path where the webhooks server listens                                            |      `/admission`      |
| `--webhook-server-ca`                  | The CA bundle to use for the webhooks server                                          |          `-`           |
| `--webhook-server-certificate`         | The certificate used by the webhooks server                                           |          `-`           |
| `--webhook-server-private-key`         | The private key used by the webhooks server                                           |          `-`           |
| `--webhook-server-autogenerate-certs`  | Generate a self signed certificate when the Secret does not exist                      |        `false`         |
| `--webhook-server-certs-secret-name`   | Secret to read the certificate from. Keys are `ca.crt`, `tls.crt` and `tls.key`        |          `-`           |
| `--kube-client-qps`                    | The QPS rate of communication between the controller and the API server               |          `5`           |
| `--kube-client-burst`                  | The burst capacity of communication between the controller and the API server         |          `10`          |
| `--enable-special-labels`              | Enable labels that perform sensitive actions                                          |        `false`         |
| `--exclude-admission-self-namespace`   | Exclude Admitik resources from admission evaluations                                  |        `false`         |
| `--excluded-admission-namespaces`      | Comma-separated list of namespaces to be excluded from admission evaluations          |          `-`           |
| `--compliance-scan-interval`           | Interval between scans of the objects that already exist. `0` disables them           |          `0`           |

## Logs

Admitik writes one JSON line per event. Every line about a decision carries the same fields, with
the same meaning as the metrics:

| Field | Meaning |
|:--|:--|
| `kind` | `validate`, `mutate` or `generate` |
| `policy` | the policy the line is about |
| `resource` | the resource type, as `group/version/resource` |
| `namespace`, `name` | the object |
| `operation` | `CREATE`, `UPDATE` or `DELETE` |
| `outcome` | `allowed`, `denied`, `permissive`, `patched`, `unchanged`, `skipped`, `generated` or `failed` |
| `reason` | `none`, `sources`, `conditions`, `template`, `patch` or `apply` |
| `duration` | how long the policy took |

The message says what the line is:

| `msg` | Level | What it says |
|:--|:--|:--|
| `admission decision` | info | The verdict of one admission request: `outcome`, `rejections` or `mutations` (the policies that shaped it), `evaluated` and `failures` |
| `policy decision` | info when the policy acted, `debug` otherwise | What one policy did with one object |

A policy that acted leaves its line at info: a rejection, a patch, a generated object or a failure.
The ones that did nothing only appear with `--zap-log-level=debug`.

Generation lines name the object that triggered them in `watched`, and the object they wrote in
`resource`, `namespace` and `name`.

> The flags of the logger are bound by controller-runtime, so `--zap-log-level` and the rest are
> also available.
