# Logs

Admitik writes one JSON line per decision: a policy of a kind, over an object, in an operation,
with a result and, when there is something to say, a message.

```json
{"level":"info","msg":"policy decision","policy":"require-limits","policyKind":"validate","operation":"CREATE","object":{"apiVersion":"apps/v1","kind":"Deployment","namespace":"team-a","name":"checkout"},"result":"denied","reason":"conditions","message":"the container 'api' must declare a cpu limit"}
```

This file is the contract. Code in `internal/telemetry/log.go`, tests in
`internal/telemetry/log_test.go`.

## Fields

| Field | Type | Meaning |
|:--|:--|:--|
| `msg` | string | `policy decision`, always. It is how a decision line is found |
| `policy` | string | The policy that was involved |
| `policyKind` | string | `validate`, `mutate` or `generate` |
| `operation` | string | `CREATE`, `UPDATE` or `DELETE` |
| `object` | object | The object: `apiVersion`, `kind`, `name`, and `namespace` when it is namespaced |
| `result` | string | What came out, see below |
| `reason` | string | Why, in a closed set: `none`, `sources`, `conditions`, `template`, `patch`, `apply` |
| `message` | string | Optional. Why, in words. Only when there is something to say |

Generation lines add one field:

| Field | Type | Meaning |
|:--|:--|:--|
| `trigger` | object | The object that woke the policy up, same shape as `object` |

Lines may also carry `duration` and, for generated objects, `created`. They are informative and may
be absent.

## Results

| `result` | Level | Meaning |
|:--|:--|:--|
| `denied` | info | The policy rejected the object |
| `permissive` | info | The policy found the object non compliant and let it pass |
| `patched` | info | The policy changed the object |
| `generated` | info | The policy wrote an object |
| `failed` | error | The policy could not be applied. `error` carries what went wrong |
| `allowed` | debug | The policy evaluated the object and had nothing to say |
| `skipped` | debug | The policy did not apply, by its conditions or because the object exists |

`info` is for what changed something. A policy that did nothing only appears with
`--zap-log-level=debug`, so the volume of the logs follows what happens in the cluster, not the
number of requests.

## Compatibility

- Fields are added, never renamed or removed. Adding a field is a minor change; renaming or
  removing one is a breaking change and needs a release note.
- New values of `result` and `reason` are added to their sets. Neither accepts free text: the
  message carries that.
- Both are checked by tests. If a test in `internal/telemetry/log_test.go` fails, the contract
  changed on purpose or by accident: decide which, and update this file in the same change.

## Writing a decision

Decisions are written by `telemetry.LogDecision`, which is the only place that knows the shape of
the line. A new kind of policy or a new place that decides something must use it:

```go
telemetry.LogDecision(logger, telemetry.DecisionLine{
	Policy:    policy.Name,
	Kind:      telemetry.KindValidate,
	Object:    common.ObjectRefOfAdmission(request.Kind, request.Namespace, request.Name),
	Operation: string(request.Operation),
	Result:    result.Outcome,
	Reason:    result.Reason,
	Message:   result.Message,
	Err:       result.Err,
})
```

The logger is the root one: the function adds every field of the line, so the caller does not
repeat them.
