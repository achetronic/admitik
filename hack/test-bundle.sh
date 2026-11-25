#!/usr/bin/env bash
#
# Installs the bundle that ships with the release and checks that it works: the Service that fronts
# the webhooks, a policy that is enforced, and nothing left behind. It runs against the cluster of
# the current kubeconfig, which has to be a throwaway one.
#
# Ref: https://github.com/admitikcore/admitik/blob/master/docs/installation.md

set -euo pipefail
cd "$(dirname "$0")/.."

# The image the bundle deploys has to be reachable by the cluster: a published tag, or one loaded
# into it. 'controller:latest', the default of the Makefile, is not pullable
IMG="${IMG:?set IMG to an image the cluster can pull}"

NAMESPACE="${NAMESPACE:-admitik}"
SERVICE="${SERVICE:-admitik-webhooks}"
POLICY_NAMESPACE="${POLICY_NAMESPACE:-bundle-scope}"
VALIDATING_CONFIGURATION="admitik-cluster-validation-policy"
MUTATING_CONFIGURATION="admitik-cluster-mutation-policy"

failed=0
pass() { printf '  \033[32mPASS\033[0m %s\n' "$*"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$*"; failed=$((failed + 1)); }

cleanup() {
	# The controller writes the webhook configurations, so they have to go before the manifest or
	# it writes them again while it shuts down
	kubectl delete validatingwebhookconfiguration "${VALIDATING_CONFIGURATION}" --ignore-not-found >/dev/null 2>&1 || true
	kubectl delete mutatingwebhookconfiguration "${MUTATING_CONFIGURATION}" --ignore-not-found >/dev/null 2>&1 || true
	kubectl delete -f dist/install.yaml --ignore-not-found >/dev/null 2>&1 || true
	kubectl delete namespace "${POLICY_NAMESPACE}" --wait=false >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "Building the bundle with ${IMG}"
make build-installer IMG="${IMG}" >/dev/null

echo "Installing it in ${NAMESPACE}"
kubectl apply -f dist/install.yaml >/dev/null

kubectl -n "${NAMESPACE}" rollout status deploy/admitik-controller-manager --timeout=180s >/dev/null \
	&& pass "the manager is running" || fail "the manager did not come up"

endpoints="$(kubectl -n "${NAMESPACE}" get endpoints "${SERVICE}" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || true)"
[ -n "${endpoints}" ] && pass "the webhooks Service has endpoints" \
	|| fail "the webhooks Service '${SERVICE}' has no endpoints"

kubectl create namespace "${POLICY_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl apply -f - >/dev/null <<-YAML
	apiVersion: admitik.dev/v1alpha1
	kind: ClusterValidationPolicy
	metadata:
	  name: bundle-check
	spec:
	  failureAction: Enforce
	  interceptedResources:
	    - group: ""
	      version: v1
	      resource: configmaps
	      operations: [CREATE]
	      namespace: ${POLICY_NAMESPACE}
	  conditions:
	    - name: deny
	      engine: plain
	      key: deny
	      value: allow
	  message:
	    engine: plain
	    template: configmaps are not allowed here
YAML

sleep 5

# A rejection is only a rejection when the policy says so. If the webhook cannot be reached, the
# object is also not created, so the reason has to be checked
inside="$(kubectl -n "${POLICY_NAMESPACE}" create configmap blocked --from-literal=a=b 2>&1 || true)"
if printf '%s' "${inside}" | grep -q "denied the request"; then
	pass "the policy rejects the object inside its scope"
elif printf '%s' "${inside}" | grep -q "failed to call webhook"; then
	fail "the webhook is unreachable: ${inside}"
else
	fail "the policy did not reject the object: ${inside}"
fi

outside="$(kubectl -n default create configmap not-scoped --from-literal=a=b 2>&1 || true)"
if printf '%s' "${outside}" | grep -q "created"; then
	pass "the rest of the cluster is left alone"
	kubectl -n default delete configmap not-scoped >/dev/null 2>&1 || true
else
	fail "the object outside the scope could not be created: ${outside}"
fi

echo "Removing the bundle"
cleanup
trap - EXIT

if kubectl -n default create configmap after-bundle --from-literal=a=b >/dev/null 2>&1; then
	pass "the cluster is usable after removing the bundle"
	kubectl -n default delete configmap after-bundle >/dev/null 2>&1 || true
else
	fail "the cluster rejects objects after removing the bundle"
fi

if [ "${failed}" -ne 0 ]; then
	echo "${failed} checks failed"
	exit 1
fi

echo "All checks passed"
