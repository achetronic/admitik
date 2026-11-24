/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package clustermutationpolicy

import (
	"context"
	"fmt"
	"slices"
	"testing"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
	policyStore "github.com/admitikcore/admitik/internal/policystore"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme for v1alpha1: %v", err)
	}
	if err := admissionregv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme for admissionregistration: %v", err)
	}

	return scheme
}

func newTestReconciler(t *testing.T, elected <-chan struct{}, objects ...client.Object) (*ClusterMutationPolicyReconciler, *policyStore.PolicyStore[*v1alpha1.ClusterMutationPolicy]) {
	t.Helper()

	scheme := testScheme(t)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	registry := policyStore.NewPolicyStore[*v1alpha1.ClusterMutationPolicy]()

	return &ClusterMutationPolicyReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Options: ClusterMutationPolicyControllerOptions{
			WebhookTimeout: 10,
			Elected:        elected,
		},
		Dependencies: ClusterMutationPolicyControllerDependencies{
			ClusterMutationPolicyRegistry: registry,
		},
	}, registry
}

func closedChannel() <-chan struct{} {
	elected := make(chan struct{})
	close(elected)

	return elected
}

func policyWith(name string, priority int, resources ...v1alpha1.AdmissionResourceGroupT) *v1alpha1.ClusterMutationPolicy {

	return &v1alpha1.ClusterMutationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ClusterMutationPolicySpec{
			Priority:             priority,
			InterceptedResources: resources,
		},
	}
}

func resourceGroup(group, resource string, operations ...admissionregv1.OperationType) v1alpha1.AdmissionResourceGroupT {
	return v1alpha1.AdmissionResourceGroupT{
		GroupVersionResource: metav1.GroupVersionResource{Group: group, Version: "v1", Resource: resource},
		Operations:           operations,
	}
}

func TestWatchedTypes(t *testing.T) {
	tests := []struct {
		name     string
		policy   *v1alpha1.ClusterMutationPolicy
		expected []string
	}{
		{
			name:     "no intercepted resources",
			policy:   policyWith("empty", 0),
			expected: []string{},
		},
		{
			name: "one resource with several operations",
			policy: policyWith("one", 0, resourceGroup("", "namespaces",
				admissionregv1.Create, admissionregv1.Update)),
			expected: []string{"/v1/namespaces/CREATE", "/v1/namespaces/UPDATE"},
		},
		{
			name: "the wildcard is expanded",
			policy: policyWith("wildcard", 0, resourceGroup("apps", "deployments",
				admissionregv1.OperationAll)),
			expected: []string{
				"apps/v1/deployments/CREATE",
				"apps/v1/deployments/UPDATE",
				"apps/v1/deployments/DELETE",
				"apps/v1/deployments/CONNECT",
			},
		},
		{
			name: "unsupported operations are skipped",
			policy: policyWith("unsupported", 0, resourceGroup("", "secrets",
				admissionregv1.Create, "NOPE")),
			expected: []string{"/v1/secrets/CREATE"},
		},
		{
			name: "the same resource twice is not duplicated",
			policy: policyWith("duplicated", 0,
				resourceGroup("", "configmaps", admissionregv1.Create),
				resourceGroup("", "configmaps", admissionregv1.Create)),
			expected: []string{"/v1/configmaps/CREATE"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := WatchedTypes(test.policy)
			if !slices.Equal(got, test.expected) {
				t.Errorf("got %v, expected %v", got, test.expected)
			}
		})
	}
}

func TestComparePolicies(t *testing.T) {
	low := policyWith("low", 10)
	high := policyWith("high", 20)
	first := policyWith("aaa", 10)
	last := policyWith("zzz", 10)

	tests := []struct {
		name     string
		a        *v1alpha1.ClusterMutationPolicy
		b        *v1alpha1.ClusterMutationPolicy
		expected int
	}{
		{name: "lower priority goes first", a: low, b: high, expected: -1},
		{name: "higher priority goes last", a: high, b: low, expected: 1},
		{name: "same priority is ordered by name", a: first, b: last, expected: -1},
		{name: "same priority, opposite order", a: last, b: first, expected: 1},
		{name: "same policy", a: low, b: low, expected: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ComparePolicies(test.a, test.b)
			if got != test.expected {
				t.Errorf("got %d, expected %d", got, test.expected)
			}
		})
	}
}

func TestSyncWebhookConfigurationCreatesTheRules(t *testing.T) {
	reconciler, _ := newTestReconciler(t, closedChannel(),
		policyWith("mutation", 10,
			resourceGroup("", "namespaces", admissionregv1.Create),
			resourceGroup("apps", "deployments", admissionregv1.OperationAll)),
	)

	if err := reconciler.SyncWebhookConfiguration(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	created := &admissionregv1.MutatingWebhookConfiguration{}
	if err := reconciler.Get(context.Background(),
		types.NamespacedName{Name: MutatingWebhookConfigurationName}, created); err != nil {
		t.Fatalf("the webhook configuration was not created: %v", err)
	}

	if len(created.Webhooks) != 1 {
		t.Fatalf("got %d webhooks, expected 1", len(created.Webhooks))
	}

	rules := created.Webhooks[0].Rules
	if len(rules) != 5 {
		t.Fatalf("got %d rules, expected 5: %v", len(rules), rules)
	}

	// The rules are sorted, so the same policies always produce the same object
	got := []string{}
	for _, rule := range rules {
		got = append(got, fmt.Sprintf("%s/%s", rule.Resources[0], rule.Operations[0]))
	}

	expected := []string{
		"namespaces/CREATE",
		"deployments/CONNECT",
		"deployments/CREATE",
		"deployments/DELETE",
		"deployments/UPDATE",
	}

	if !slices.Equal(got, expected) {
		t.Errorf("got %v, expected %v", got, expected)
	}
}

func TestSyncWebhookConfigurationFollowsThePolicies(t *testing.T) {
	reconciler, _ := newTestReconciler(t, closedChannel(),
		policyWith("mutation", 0, resourceGroup("", "namespaces", admissionregv1.Create)),
	)

	ctx := context.Background()
	if err := reconciler.SyncWebhookConfiguration(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A policy disappears: the configuration has to shrink
	if err := reconciler.Delete(ctx, &v1alpha1.ClusterMutationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "mutation"},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := reconciler.SyncWebhookConfiguration(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	updated := &admissionregv1.MutatingWebhookConfiguration{}
	if err := reconciler.Get(ctx, types.NamespacedName{Name: MutatingWebhookConfigurationName}, updated); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(updated.Webhooks[0].Rules) != 0 {
		t.Errorf("got %d rules, expected none after deleting the only policy", len(updated.Webhooks[0].Rules))
	}
}

func TestReconcileKeepsTheStoreUpdatedWhenNotLeader(t *testing.T) {
	notElected := make(chan struct{})
	policy := policyWith("mutation", 0, resourceGroup("", "namespaces", admissionregv1.Create))

	reconciler, registry := newTestReconciler(t, notElected, policy)

	if err := reconciler.ReconcileClusterMutationPolicy(context.Background(), watch.Modified, policy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Every replica needs the policy in the store: that is what the admission server reads
	stored := registry.Store.GetResources("/v1/namespaces/CREATE")
	if len(stored) != 1 {
		t.Fatalf("got %d policies in the store, expected 1", len(stored))
	}
}

func TestReconcileDoesNotWriteTheWebhookWhenNotLeader(t *testing.T) {
	notElected := make(chan struct{})
	policy := policyWith("mutation", 0, resourceGroup("", "namespaces", admissionregv1.Create))

	reconciler, _ := newTestReconciler(t, notElected, policy)

	if err := reconciler.ReconcileClusterMutationPolicy(context.Background(), watch.Modified, policy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := reconciler.Get(context.Background(),
		types.NamespacedName{Name: MutatingWebhookConfigurationName}, &admissionregv1.MutatingWebhookConfiguration{})
	if err == nil {
		t.Error("a replica that is not the leader wrote the webhook configuration")
	}
}

func TestReconcileWritesTheWebhookWhenLeader(t *testing.T) {
	policy := policyWith("mutation", 0, resourceGroup("", "namespaces", admissionregv1.Create))
	reconciler, _ := newTestReconciler(t, closedChannel(), policy)

	if err := reconciler.ReconcileClusterMutationPolicy(context.Background(), watch.Modified, policy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := reconciler.Get(context.Background(),
		types.NamespacedName{Name: MutatingWebhookConfigurationName}, &admissionregv1.MutatingWebhookConfiguration{})
	if err != nil {
		t.Errorf("the leader did not write the webhook configuration: %v", err)
	}
}

func TestReconcileOrdersTheCollectionByPriorityAndName(t *testing.T) {
	policies := []*v1alpha1.ClusterMutationPolicy{
		policyWith("zzz", 10, resourceGroup("", "namespaces", admissionregv1.Create)),
		policyWith("aaa", 10, resourceGroup("", "namespaces", admissionregv1.Create)),
		policyWith("mmm", 5, resourceGroup("", "namespaces", admissionregv1.Create)),
	}

	for _, policy := range policies {
		reconciler, registry := newTestReconciler(t, closedChannel(), policy)

		if err := reconciler.ReconcileClusterMutationPolicy(context.Background(), watch.Modified, policy); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		stored := registry.Store.GetResources("/v1/namespaces/CREATE")
		if len(stored) != 1 {
			t.Fatalf("got %d policies, expected 1", len(stored))
		}
	}

	// Reconciling them in one store, in a different order, must produce the same result
	reconciler, registry := newTestReconciler(t, closedChannel())
	for _, policy := range []*v1alpha1.ClusterMutationPolicy{policies[2], policies[0], policies[1]} {
		if err := reconciler.ReconcileClusterMutationPolicy(context.Background(), watch.Modified, policy); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	stored := registry.Store.GetResources("/v1/namespaces/CREATE")
	got := []string{}
	for _, policy := range stored {
		got = append(got, policy.Name)
	}

	expected := []string{"mmm", "aaa", "zzz"}
	if !slices.Equal(got, expected) {
		t.Errorf("got %v, expected %v", got, expected)
	}
}
