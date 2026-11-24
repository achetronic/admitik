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

package kubeapply

import (
	"context"
	"testing"

	//
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicFake "k8s.io/client-go/dynamic/fake"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
)

var configMapsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

func newTestWriter(t *testing.T, objects ...runtime.Object) (*Writer, *dynamicFake.FakeDynamicClient) {
	t.Helper()

	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)

	// The fake client needs the typed kind registered to apply over it
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	client := dynamicFake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{configMapsGVR: "ConfigMapList"},
		objects...,
	)

	return NewWriter(client, mapper), client
}

// objectName is the name of the object written by these tests
const objectName = "generated"

func configMap(labels map[string]any) *unstructured.Unstructured {

	metadata := map[string]any{"name": objectName, "namespace": "default"}
	if labels != nil {
		metadata["labels"] = labels
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   metadata,
		"data":       map[string]any{"key": "value"},
	}}
}

func TestApplyCreatesTheObject(t *testing.T) {
	writer, client := newTestWriter(t)

	result, err := writer.Apply(context.Background(), Options{
		PolicyName: "my-policy",
		PolicyKind: "ClusterGenerationPolicy",
	}, configMap(nil).Object)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Created {
		t.Error("the object should have been created")
	}
	if result.Resource != "configmaps" || result.Name != "generated" || result.Namespace != "default" {
		t.Errorf("got %+v, expected the configmap coordinates", result)
	}

	stored, err := client.Resource(configMapsGVR).Namespace("default").
		Get(context.Background(), "generated", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("the object was not created: %v", err)
	}

	labels := stored.GetLabels()
	if labels[v1alpha1.ManagedByLabel] != v1alpha1.ManagedByValue {
		t.Errorf("got %q as managed-by", labels[v1alpha1.ManagedByLabel])
	}
	if labels[v1alpha1.PolicyLabel] != "my-policy" {
		t.Errorf("got %q as policy", labels[v1alpha1.PolicyLabel])
	}
	if labels[v1alpha1.PolicyKindLabel] != "ClusterGenerationPolicy" {
		t.Errorf("got %q as policy kind", labels[v1alpha1.PolicyKindLabel])
	}
}

func TestApplyKeepsTheLabelsOfTheTemplate(t *testing.T) {
	writer, client := newTestWriter(t)

	object := configMap(map[string]any{"team": "platform"})

	if _, err := writer.Apply(context.Background(), Options{PolicyName: "my-policy"}, object.Object); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stored, err := client.Resource(configMapsGVR).Namespace("default").
		Get(context.Background(), "generated", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if labels := stored.GetLabels(); labels["team"] != "platform" {
		t.Errorf("the labels of the template were lost: %v", labels)
	}
}

func TestApplyLeavesAnExistingObjectAlone(t *testing.T) {
	writer, client := newTestWriter(t, configMap(nil))

	result, err := writer.Apply(context.Background(), Options{PolicyName: "my-policy"},
		configMap(nil).Object)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Existing {
		t.Error("the object already existed and overwriting was disabled")
	}
	if result.Created {
		t.Error("nothing should have been created")
	}

	stored, err := client.Resource(configMapsGVR).Namespace("default").
		Get(context.Background(), "generated", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stored.GetLabels()) != 0 {
		t.Errorf("the existing object was modified: %v", stored.GetLabels())
	}
}

// The takeover of an existing object is not covered here: the fake dynamic client cannot apply
// over an unstructured object. It needs a real API server.

func TestApplyWithAnUnknownKind(t *testing.T) {
	writer, _ := newTestWriter(t)

	object := map[string]any{
		"apiVersion": "example.com/v1",
		"kind":       "Widget",
		"metadata":   map[string]any{"name": "unknown", "namespace": "default"},
	}

	if _, err := writer.Apply(context.Background(), Options{}, object); err == nil {
		t.Error("expected an error for a kind the cluster does not have")
	}
}
