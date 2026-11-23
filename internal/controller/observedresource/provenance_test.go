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

package observedresource

import (
	"testing"

	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
)

// policyName is the policy used by these tests
const policyName = "my-policy"

func testGeneratedObject(labels map[string]any) *unstructured.Unstructured {

	metadata := map[string]any{"name": "generated", "namespace": "default"}
	if labels != nil {
		metadata["labels"] = labels
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   metadata,
	}}
}

func TestMarkProvenance(t *testing.T) {
	policy := &v1alpha1.ClusterGenerationPolicy{ObjectMeta: metav1.ObjectMeta{Name: policyName}}

	object := testGeneratedObject(nil)
	markProvenance(object, policy)

	labels := object.GetLabels()
	if labels[v1alpha1.ManagedByLabel] != v1alpha1.ManagedByValue {
		t.Errorf("got %q as managed-by, expected %q", labels[v1alpha1.ManagedByLabel], v1alpha1.ManagedByValue)
	}
	if labels[v1alpha1.PolicyLabel] != policyName {
		t.Errorf("got %q as policy, expected the policy name", labels[v1alpha1.PolicyLabel])
	}
	if labels[v1alpha1.PolicyKindLabel] != "ClusterGenerationPolicy" {
		t.Errorf("got %q as policy kind", labels[v1alpha1.PolicyKindLabel])
	}
}

func TestMarkProvenanceKeepsTheLabelsOfTheTemplate(t *testing.T) {
	policy := &v1alpha1.ClusterGenerationPolicy{ObjectMeta: metav1.ObjectMeta{Name: policyName}}

	object := testGeneratedObject(map[string]any{"team": "platform"})
	markProvenance(object, policy)

	labels := object.GetLabels()
	if labels["team"] != "platform" {
		t.Errorf("the labels of the template were lost: %v", labels)
	}
	if labels[v1alpha1.PolicyLabel] != policyName {
		t.Errorf("the provenance was not added: %v", labels)
	}
}

func TestMarkProvenanceOverwritesAForeignPolicy(t *testing.T) {
	policy := &v1alpha1.ClusterGenerationPolicy{ObjectMeta: metav1.ObjectMeta{Name: policyName}}

	object := testGeneratedObject(map[string]any{v1alpha1.PolicyLabel: "another-policy"})
	markProvenance(object, policy)

	if labels := object.GetLabels(); labels[v1alpha1.PolicyLabel] != policyName {
		t.Errorf("got %q, expected the policy that is writing the object", labels[v1alpha1.PolicyLabel])
	}
}
