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

package sources

import (
	"context"
	"slices"
	"testing"

	//
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicFake "k8s.io/client-go/dynamic/fake"

	//
	"github.com/freepik-company/admitik/internal/informer"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
)

type testSourceProvider struct {
	sources []string
}

func (p *testSourceProvider) GetReferencedSources() []string {
	return p.sources
}

func newTestController(t *testing.T, providers ...*testSourceProvider) *SourcesController {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	policyStores := make([]policyStore.SourceProvider, 0, len(providers))
	for _, provider := range providers {
		policyStores = append(policyStores, provider)
	}

	// Every resource type used by these tests needs its list kind registered
	listKinds := map[schema.GroupVersionResource]string{
		{Group: "", Version: "v1", Resource: "configmaps"}: "ConfigMapList",
		{Group: "", Version: "v1", Resource: "secrets"}:    "SecretList",
	}

	return &SourcesController{
		Dependencies: SourcesControllerDependencies{
			Context:      &ctx,
			PolicyStores: policyStores,
			Pool: informer.NewPool(&ctx,
				dynamicFake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds),
				0),
		},
	}
}

func TestGetSourcesFromPolicies(t *testing.T) {
	controller := newTestController(t,
		&testSourceProvider{sources: []string{"/v1/configmaps", "apps/v1/deployments"}},
		&testSourceProvider{sources: []string{"/v1/configmaps", "/v1/secrets"}},
	)

	got := controller.getSourcesFromPolicies()
	expected := []string{"/v1/configmaps", "/v1/secrets", "apps/v1/deployments"}

	if !slices.Equal(got, expected) {
		t.Errorf("got %v, expected %v", got, expected)
	}
}

func TestGetSourcesFromPoliciesWithoutStores(t *testing.T) {
	controller := newTestController(t)

	if got := controller.getSourcesFromPolicies(); len(got) != 0 {
		t.Errorf("got %v, expected an empty list", got)
	}
}

func TestLaunchDesiredInformersStartsOneInformerPerSource(t *testing.T) {
	controller := newTestController(t,
		&testSourceProvider{sources: []string{"/v1/configmaps", "/v1/secrets"}},
	)

	controller.launchDesiredInformers()

	owned := controller.Dependencies.Pool.Owned(consumerName)
	if len(owned) != 2 {
		t.Fatalf("got %d scopes owned, expected 2 (%v)", len(owned), owned)
	}

	for _, scope := range owned {
		inform := controller.Dependencies.Pool.Get(scope)
		if inform == nil {
			t.Fatalf("informer for %v is nil", scope)
		}
		if !inform.IsStarted() {
			t.Errorf("informer for %v was not started", scope)
		}
	}
}

func TestLaunchDesiredInformersIsIdempotent(t *testing.T) {
	controller := newTestController(t, &testSourceProvider{sources: []string{"/v1/configmaps"}})

	controller.launchDesiredInformers()
	scope := informer.Scope{GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}}
	first := controller.Dependencies.Pool.Get(scope)

	controller.launchDesiredInformers()
	second := controller.Dependencies.Pool.Get(scope)

	if first != second {
		t.Error("the informer was rebuilt although the source did not change")
	}
}

func TestLaunchDesiredInformersSkipsInvalidSources(t *testing.T) {
	controller := newTestController(t, &testSourceProvider{sources: []string{"invalid", "/v1/configmaps"}})

	controller.launchDesiredInformers()

	owned := controller.Dependencies.Pool.Owned(consumerName)
	if len(owned) != 1 {
		t.Fatalf("got %d scopes owned, expected 1 (%v)", len(owned), owned)
	}
	if owned[0].GVR.Resource != "configmaps" {
		t.Errorf("got %v, expected the configmaps scope", owned[0])
	}
}

func TestCleanNotNeededInformers(t *testing.T) {
	controller := newTestController(t, &testSourceProvider{sources: []string{"/v1/configmaps"}})
	controller.launchDesiredInformers()

	// Drop the source and reconcile again
	controller.Dependencies.PolicyStores[0].(*testSourceProvider).sources = nil
	controller.cleanNotNeededInformers()

	if owned := controller.Dependencies.Pool.Owned(consumerName); len(owned) != 0 {
		t.Errorf("got %v, expected no owned scopes", owned)
	}
}

func TestCleanNotNeededInformersKeepsOtherConsumers(t *testing.T) {
	controller := newTestController(t, &testSourceProvider{sources: []string{"/v1/configmaps"}})
	controller.launchDesiredInformers()

	// Another consumer asks for the same resource
	scope := informer.Scope{GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}}
	if _, err := controller.Dependencies.Pool.Ensure("another", scope); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	controller.Dependencies.PolicyStores[0].(*testSourceProvider).sources = nil
	controller.cleanNotNeededInformers()

	// This controller is gone, but the informer has to stay for the other one
	if owned := controller.Dependencies.Pool.Owned(consumerName); len(owned) != 0 {
		t.Errorf("got %v, expected no owned scopes", owned)
	}
	if controller.Dependencies.Pool.Get(scope) == nil {
		t.Error("the informer was stopped although another consumer was using it")
	}
}

func TestGvrFromString(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		expected    schema.GroupVersionResource
		expectError bool
	}{
		{
			name:     "core group",
			source:   "/v1/configmaps",
			expected: schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"},
		},
		{
			name:     "named group",
			source:   "apps/v1/deployments",
			expected: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		},
		{name: "too few parts", source: "/v1", expectError: true},
		{name: "too many parts", source: "/v1/configmaps/CREATE", expectError: true},
		{name: "empty", source: "", expectError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := gvrFromString(test.source)

			if test.expectError {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != test.expected {
				t.Errorf("got %v, expected %v", got, test.expected)
			}
		})
	}
}
