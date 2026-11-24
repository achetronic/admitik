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

package policystore

import (
	"slices"
	"testing"

	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
)

// The policy store must be usable by consumers that only care about sources
var _ SourceProvider = &PolicyStore[*testPolicy]{}

type testPolicy struct {
	name    string
	sources []v1alpha1.SourceGroupT
}

func (p *testPolicy) GetUniqueIdentifier() string {
	return p.name
}

func (p *testPolicy) GetSources() []v1alpha1.SourceGroupT {
	return p.sources
}

func source(group, resource string) v1alpha1.SourceGroupT {
	return v1alpha1.SourceGroupT{
		GroupVersionResource: metav1.GroupVersionResource{
			Group:    group,
			Version:  "v1",
			Resource: resource,
		},
	}
}

func TestGetReferencedSources(t *testing.T) {
	tests := []struct {
		name     string
		policies map[string][]*testPolicy
		expected []string
	}{
		{
			name:     "returns an empty list when there are no policies",
			policies: map[string][]*testPolicy{},
			expected: []string{},
		},
		{
			name: "returns the sources of every collection",
			policies: map[string][]*testPolicy{
				"/v1/configmaps/CREATE": {{name: "a", sources: []v1alpha1.SourceGroupT{source("", "secrets")}}},
				"apps/v1/deployments/CREATE": {
					{name: "b", sources: []v1alpha1.SourceGroupT{source("apps", "replicasets")}},
				},
			},
			expected: []string{"/v1/secrets", "apps/v1/replicasets"},
		},
		{
			name: "removes duplicated sources",
			policies: map[string][]*testPolicy{
				"/v1/configmaps/CREATE": {
					{name: "a", sources: []v1alpha1.SourceGroupT{source("", "secrets")}},
					{name: "b", sources: []v1alpha1.SourceGroupT{source("", "secrets")}},
				},
			},
			expected: []string{"/v1/secrets"},
		},
		{
			name: "ignores empty sources",
			policies: map[string][]*testPolicy{
				"/v1/configmaps/CREATE": {{name: "a", sources: []v1alpha1.SourceGroupT{{}}}},
			},
			expected: []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := NewPolicyStore[*testPolicy]()
			for collectionName, policies := range test.policies {
				for _, policy := range policies {
					store.AddOrUpdateResource(collectionName, policy)
				}
			}

			got := store.GetReferencedSources()
			if got == nil {
				t.Fatal("got nil, expected an empty list")
			}
			if !slices.Equal(got, test.expected) {
				t.Errorf("got %v, expected %v", got, test.expected)
			}
		})
	}
}

func TestPolicyStoreKeepsPoliciesPerCollection(t *testing.T) {
	store := NewPolicyStore[*testPolicy]()

	store.AddOrUpdateResource("/v1/configmaps/CREATE", &testPolicy{name: "a"})
	store.AddOrUpdateResource("/v1/secrets/CREATE", &testPolicy{name: "b"})

	if got := len(store.GetResources("/v1/configmaps/CREATE")); got != 1 {
		t.Errorf("got %d policies for configmaps, expected 1", got)
	}
	if got := len(store.GetResources("/v1/secrets/CREATE")); got != 1 {
		t.Errorf("got %d policies for secrets, expected 1", got)
	}
}

func TestPoliciesCountsEachPolicyOnce(t *testing.T) {
	store := NewPolicyStore[*testPolicy]()

	// The same policy is indexed under every resource type it watches
	store.Store.AddOrUpdateResource("v1/configmaps/CREATE", &testPolicy{name: "a"})
	store.Store.AddOrUpdateResource("v1/secrets/CREATE", &testPolicy{name: "a"})
	store.Store.AddOrUpdateResource("v1/configmaps/CREATE", &testPolicy{name: "b"})

	if got := store.Policies(); got != 2 {
		t.Errorf("got %d policies, expected 2", got)
	}
}

func TestPoliciesOfAnEmptyStore(t *testing.T) {
	if got := NewPolicyStore[*testPolicy]().Policies(); got != 0 {
		t.Errorf("got %d policies, expected 0", got)
	}
}
