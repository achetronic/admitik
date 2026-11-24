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

package clustergenerationpolicy

import (
	"context"
	"errors"
	"testing"

	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
)

// spyDeleter records the calls it receives
type spyDeleter struct {
	calls int
	kind  string
	name  string
	err   error
}

func (s *spyDeleter) Delete(_ context.Context, policyKind, policyName string) (int, error) {
	s.calls++
	s.kind = policyKind
	s.name = policyName

	if s.err != nil {
		return 0, s.err
	}

	return 1, nil
}

func newTestReconciler(t *testing.T, deleter Deleter) *ClusterGenerationPolicyReconciler {
	t.Helper()

	return &ClusterGenerationPolicyReconciler{
		Dependencies: ClusterGenerationPolicyControllerDependencies{
			ClusterGenerationPolicyRegistry: policyStore.NewPolicyStore[*v1alpha1.ClusterGenerationPolicy](),
			Cascade:                         deleter,
		},
	}
}

// policyName is the policy used by these tests
const policyName = "my-policy"

func policyWithDeletionPolicy(deletionPolicy string) *v1alpha1.ClusterGenerationPolicy {
	return &v1alpha1.ClusterGenerationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: policyName},
		Spec: v1alpha1.ClusterGenerationPolicySpec{
			DeletionPolicy: deletionPolicy,
		},
	}
}

func TestDeletionWithCascadeRemovesTheGeneratedObjects(t *testing.T) {
	deleter := &spyDeleter{}
	reconciler := newTestReconciler(t, deleter)

	policy := policyWithDeletionPolicy(v1alpha1.DeletionPolicyDelete)

	if err := reconciler.ReconcileClusterGenerationPolicy(context.Background(), watch.Deleted, policy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deleter.calls != 1 {
		t.Fatalf("got %d calls, expected one", deleter.calls)
	}
	if deleter.kind != "ClusterGenerationPolicy" || deleter.name != "my-policy" {
		t.Errorf("got %s/%s, expected ClusterGenerationPolicy/my-policy", deleter.kind, deleter.name)
	}
}

func TestDeletionWithKeepLeavesTheGeneratedObjects(t *testing.T) {
	tests := []struct {
		name           string
		deletionPolicy string
	}{
		{name: "keep", deletionPolicy: v1alpha1.DeletionPolicyKeep},
		{name: "empty, which means keep", deletionPolicy: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deleter := &spyDeleter{}
			reconciler := newTestReconciler(t, deleter)

			policy := policyWithDeletionPolicy(test.deletionPolicy)

			if err := reconciler.ReconcileClusterGenerationPolicy(context.Background(), watch.Deleted, policy); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if deleter.calls != 0 {
				t.Errorf("got %d calls, expected none", deleter.calls)
			}
		})
	}
}

func TestUpdateDoesNotRemoveTheGeneratedObjects(t *testing.T) {
	deleter := &spyDeleter{}
	reconciler := newTestReconciler(t, deleter)

	policy := policyWithDeletionPolicy(v1alpha1.DeletionPolicyDelete)

	if err := reconciler.ReconcileClusterGenerationPolicy(context.Background(), watch.Modified, policy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deleter.calls != 0 {
		t.Errorf("got %d calls, expected none: the policy was not deleted", deleter.calls)
	}
}

func TestDeletionWithoutCascadeConfigured(t *testing.T) {
	reconciler := newTestReconciler(t, nil)

	policy := policyWithDeletionPolicy(v1alpha1.DeletionPolicyDelete)

	if err := reconciler.ReconcileClusterGenerationPolicy(context.Background(), watch.Deleted, policy); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeletionFailsWhenTheCascadeFails(t *testing.T) {
	deleter := &spyDeleter{err: errors.New("boom")}
	reconciler := newTestReconciler(t, deleter)

	policy := policyWithDeletionPolicy(v1alpha1.DeletionPolicyDelete)

	// The error has to reach the controller, so the finalizer is not removed and it retries
	if err := reconciler.ReconcileClusterGenerationPolicy(context.Background(), watch.Deleted, policy); err == nil {
		t.Error("expected an error")
	}
}
