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
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	sourcesRegistry "github.com/freepik-company/admitik/internal/registry/sources"
)

type testSourceProvider struct {
	sources []string
}

func (p *testSourceProvider) GetReferencedSources() []string {
	return p.sources
}

func newTestController(t *testing.T, providers ...*testSourceProvider) *SourcesController {
	t.Helper()

	ctx := context.Background()
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
			Context:         &ctx,
			PolicyStores:    policyStores,
			SourcesRegistry: sourcesRegistry.NewSourcesRegistry(),
			Client:          dynamicFake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds),
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

	registered := controller.Dependencies.SourcesRegistry.GetRegisteredResourceTypes()
	if len(registered) != 2 {
		t.Fatalf("got %d informers registered, expected 2 (%v)", len(registered), registered)
	}

	for _, gvr := range registered {
		inform := controller.Dependencies.SourcesRegistry.GetInformer(gvr)
		if inform == nil {
			t.Fatalf("informer for %v is nil", gvr)
		}
		if !inform.IsStarted() {
			t.Errorf("informer for %v was not started", gvr)
		}
	}
}

func TestLaunchDesiredInformersIsIdempotent(t *testing.T) {
	controller := newTestController(t, &testSourceProvider{sources: []string{"/v1/configmaps"}})

	controller.launchDesiredInformers()
	first := controller.Dependencies.SourcesRegistry.GetInformer(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"})

	controller.launchDesiredInformers()
	second := controller.Dependencies.SourcesRegistry.GetInformer(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"})

	if first != second {
		t.Error("the informer was rebuilt although the source did not change")
	}
}

func TestLaunchDesiredInformersSkipsInvalidSources(t *testing.T) {
	controller := newTestController(t, &testSourceProvider{sources: []string{"invalid", "/v1/configmaps"}})

	controller.launchDesiredInformers()

	registered := controller.Dependencies.SourcesRegistry.GetRegisteredResourceTypes()
	if len(registered) != 1 {
		t.Fatalf("got %d informers registered, expected 1 (%v)", len(registered), registered)
	}
	if registered[0].Resource != "configmaps" {
		t.Errorf("got %v, expected the configmaps informer", registered[0])
	}
}

func TestCleanNotNeededInformers(t *testing.T) {
	controller := newTestController(t, &testSourceProvider{sources: []string{"/v1/configmaps"}})
	controller.launchDesiredInformers()

	// Drop the source and reconcile again
	controller.Dependencies.PolicyStores[0].(*testSourceProvider).sources = nil
	controller.cleanNotNeededInformers()

	if registered := controller.Dependencies.SourcesRegistry.GetRegisteredResourceTypes(); len(registered) != 0 {
		t.Errorf("got %v, expected no registered informers", registered)
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
