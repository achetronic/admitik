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

package clustervalidationpolicy

import (
	"context"
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

func newTestReconciler(t *testing.T, elected <-chan struct{}, objects ...client.Object) (*ClusterValidationPolicyReconciler, *policyStore.PolicyStore[*v1alpha1.ClusterValidationPolicy]) {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme for v1alpha1: %v", err)
	}
	if err := admissionregv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme for admissionregistration: %v", err)
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	registry := policyStore.NewPolicyStore[*v1alpha1.ClusterValidationPolicy]()

	return &ClusterValidationPolicyReconciler{
		Client: fakeClient,
		Scheme: scheme,
		Options: ClusterValidationPolicyControllerOptions{
			WebhookTimeout: 10,
			Elected:        elected,
		},
		Dependencies: ClusterValidationPolicyControllerDependencies{
			ClusterValidationPolicyRegistry: registry,
		},
	}, registry
}

func closedChannel() <-chan struct{} {
	elected := make(chan struct{})
	close(elected)

	return elected
}

func policyWith(name string, resources ...v1alpha1.AdmissionResourceGroupT) *v1alpha1.ClusterValidationPolicy {

	return &v1alpha1.ClusterValidationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ClusterValidationPolicySpec{
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
		policy   *v1alpha1.ClusterValidationPolicy
		expected []string
	}{
		{
			name:     "one resource with several operations",
			policy:   policyWith("one", resourceGroup("", "namespaces", admissionregv1.Create, admissionregv1.Update)),
			expected: []string{"/v1/namespaces/CREATE", "/v1/namespaces/UPDATE"},
		},
		{
			name:   "the wildcard is expanded",
			policy: policyWith("wildcard", resourceGroup("apps", "deployments", admissionregv1.OperationAll)),
			expected: []string{
				"apps/v1/deployments/CREATE",
				"apps/v1/deployments/UPDATE",
				"apps/v1/deployments/DELETE",
				"apps/v1/deployments/CONNECT",
			},
		},
		{
			name:     "unsupported operations are skipped",
			policy:   policyWith("unsupported", resourceGroup("", "secrets", admissionregv1.Create, "NOPE")),
			expected: []string{"/v1/secrets/CREATE"},
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

func TestReconcileKeepsTheStoreUpdatedWhenNotLeader(t *testing.T) {
	notElected := make(chan struct{})
	policy := policyWith("validation", resourceGroup("", "namespaces", admissionregv1.Create))

	reconciler, registry := newTestReconciler(t, notElected, policy)

	if err := reconciler.ReconcileClusterValidationPolicy(context.Background(), watch.Modified, policy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if stored := registry.Store.GetResources("/v1/namespaces/CREATE"); len(stored) != 1 {
		t.Fatalf("got %d policies in the store, expected 1", len(stored))
	}
}

func TestReconcileDoesNotWriteTheWebhookWhenNotLeader(t *testing.T) {
	notElected := make(chan struct{})
	policy := policyWith("validation", resourceGroup("", "namespaces", admissionregv1.Create))

	reconciler, _ := newTestReconciler(t, notElected, policy)

	if err := reconciler.ReconcileClusterValidationPolicy(context.Background(), watch.Modified, policy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := reconciler.Get(context.Background(),
		types.NamespacedName{Name: ValidatingWebhookConfigurationName}, &admissionregv1.ValidatingWebhookConfiguration{})
	if err == nil {
		t.Error("a replica that is not the leader wrote the webhook configuration")
	}
}

func TestSyncWebhookConfigurationCreatesTheRules(t *testing.T) {
	reconciler, _ := newTestReconciler(t, closedChannel(),
		policyWith("validation",
			resourceGroup("", "namespaces", admissionregv1.Create),
			resourceGroup("apps", "deployments", admissionregv1.OperationAll)),
	)

	if err := reconciler.SyncWebhookConfiguration(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	created := &admissionregv1.ValidatingWebhookConfiguration{}
	if err := reconciler.Get(context.Background(),
		types.NamespacedName{Name: ValidatingWebhookConfigurationName}, created); err != nil {
		t.Fatalf("the webhook configuration was not created: %v", err)
	}

	if len(created.Webhooks[0].Rules) != 5 {
		t.Errorf("got %d rules, expected 5: %v", len(created.Webhooks[0].Rules), created.Webhooks[0].Rules)
	}
}
