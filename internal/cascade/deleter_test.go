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

package cascade

import (
	"context"
	"testing"

	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
)

var (
	configMapsGVR   = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}
	clusterRolesGVR = schema.GroupVersionResource{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}
)

// discoveryWith returns a discovery client that knows two resource types
func discoveryWith(t *testing.T, verbs ...string) *fakediscovery.FakeDiscovery {
	t.Helper()

	fake := &fakediscovery.FakeDiscovery{Fake: &k8stesting.Fake{}}
	fake.Resources = []*metav1.APIResourceList{
		{
			GroupVersion: "v1",
			APIResources: []metav1.APIResource{
				{Name: "configmaps", Namespaced: true, Verbs: verbs},
				{Name: "configmaps/status", Namespaced: true, Verbs: verbs},
				{Name: "events", Namespaced: true, Verbs: verbs},
			},
		},
		{
			GroupVersion: "rbac.authorization.k8s.io/v1",
			APIResources: []metav1.APIResource{
				{Name: "clusterroles", Namespaced: false, Verbs: verbs},
			},
		},
	}

	return fake
}

func object(gvr schema.GroupVersionResource, kind, name, namespace string, labels map[string]any) *unstructured.Unstructured {

	metadata := map[string]any{"name": name}
	if namespace != "" {
		metadata["namespace"] = namespace
	}
	if labels != nil {
		metadata["labels"] = labels
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gvr.GroupVersion().String(),
		"kind":       kind,
		"metadata":   metadata,
	}}
}

func provenance(policyName string) map[string]any {
	return map[string]any{
		v1alpha1.ManagedByLabel:  v1alpha1.ManagedByValue,
		v1alpha1.PolicyLabel:     policyName,
		v1alpha1.PolicyKindLabel: "ClusterGenerationPolicy",
	}
}

func newTestDeleter(t *testing.T, discovery *fakediscovery.FakeDiscovery, objects ...runtime.Object) (*Deleter, *dynamicFake.FakeDynamicClient) {
	t.Helper()

	client := dynamicFake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{
			configMapsGVR:   "ConfigMapList",
			clusterRolesGVR: "ClusterRoleList",
		},
		objects...,
	)

	return New(client, discovery), client
}

func TestDeleteRemovesEveryObjectOfThePolicy(t *testing.T) {
	discovery := discoveryWith(t, "list", "delete", "get")
	deleter, client := newTestDeleter(t, discovery,
		object(configMapsGVR, "ConfigMap", "mine", "default", provenance("my-policy")),
		object(configMapsGVR, "ConfigMap", "mine-in-another-namespace", "other", provenance("my-policy")),
		object(configMapsGVR, "ConfigMap", "of-another-policy", "default", provenance("another-policy")),
		object(configMapsGVR, "ConfigMap", "of-nobody", "default", nil),
		object(clusterRolesGVR, "ClusterRole", "mine-cluster-scoped", "", provenance("my-policy")),
	)

	deleted, err := deleter.Delete(context.Background(), "ClusterGenerationPolicy", "my-policy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deleted != 3 {
		t.Errorf("got %d objects deleted, expected 3", deleted)
	}

	// The objects of other policies and the ones without the label are still there
	for _, expected := range []struct {
		gvr       schema.GroupVersionResource
		namespace string
		name      string
	}{
		{configMapsGVR, "default", "of-another-policy"},
		{configMapsGVR, "default", "of-nobody"},
	} {
		if _, err := client.Resource(expected.gvr).Namespace(expected.namespace).
			Get(context.Background(), expected.name, metav1.GetOptions{}); err != nil {
			t.Errorf("the object %s was deleted and should not be: %v", expected.name, err)
		}
	}
}

func TestDeleteWithoutObjects(t *testing.T) {
	discovery := discoveryWith(t, "list", "delete", "get")
	deleter, _ := newTestDeleter(t, discovery)

	deleted, err := deleter.Delete(context.Background(), "ClusterGenerationPolicy", "my-policy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deleted != 0 {
		t.Errorf("got %d objects deleted, expected none", deleted)
	}
}

func TestDeleteSkipsTheTypesThatCannotBeListedOrDeleted(t *testing.T) {
	discovery := discoveryWith(t, "get")
	deleter, _ := newTestDeleter(t, discovery,
		object(configMapsGVR, "ConfigMap", "mine", "default", provenance("my-policy")),
	)

	deleted, err := deleter.Delete(context.Background(), "ClusterGenerationPolicy", "my-policy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deleted != 0 {
		t.Errorf("got %d objects deleted, expected none: the types were not listable", deleted)
	}
}

func TestResourceTypesLeavesOutSubresourcesAndEvents(t *testing.T) {
	deleter, _ := newTestDeleter(t, discoveryWith(t, "list", "delete"))

	resources, err := deleter.resourceTypes()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := []string{}
	for _, resource := range resources {
		got = append(got, resource.Resource)
	}

	for _, unexpected := range []string{"configmaps/status", "events"} {
		for _, resource := range got {
			if resource == unexpected {
				t.Errorf("%s should not be swept", unexpected)
			}
		}
	}

	if len(got) != 2 {
		t.Errorf("got %v, expected configmaps and clusterroles", got)
	}
}

func TestSelectorFor(t *testing.T) {
	selector := selectorFor("ClusterGenerationPolicy", "my-policy")

	expected := "admitik.dev/managed-by=admitik,admitik.dev/policy=my-policy,admitik.dev/policy-kind=ClusterGenerationPolicy"
	if selector != expected {
		t.Errorf("got %q, expected %q", selector, expected)
	}
}
