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

package admission

import (
	"testing"

	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
)

func scope(namespace, name string) v1alpha1.AdmissionResourceGroupT {
	return v1alpha1.AdmissionResourceGroupT{
		GroupVersionResource: metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		Namespace:            namespace,
		Name:                 name,
	}
}

func TestMatchesScopes(t *testing.T) {
	resource := metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"}
	other := metav1.GroupVersionResource{Version: "v1", Resource: "secrets"}

	cases := map[string]struct {
		scopes    []v1alpha1.AdmissionResourceGroupT
		resource  metav1.GroupVersionResource
		namespace string
		name      string
		expected  bool
	}{
		"sin filtros cubre cualquier objeto": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("", "")},
			resource: resource, namespace: "team-a", name: "one", expected: true,
		},
		"un namespace concreto cubre sus objetos": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("team-a", "")},
			resource: resource, namespace: "team-a", name: "one", expected: true,
		},
		"un namespace concreto no cubre otros": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("team-a", "")},
			resource: resource, namespace: "team-b", name: "one", expected: false,
		},
		"un nombre concreto cubre ese objeto en cualquier namespace": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("", "one")},
			resource: resource, namespace: "team-b", name: "one", expected: true,
		},
		"un nombre concreto no cubre otro nombre": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("", "one")},
			resource: resource, namespace: "team-a", name: "two", expected: false,
		},
		"namespace y nombre tienen que coincidir los dos": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("team-a", "one")},
			resource: resource, namespace: "team-a", name: "two", expected: false,
		},
		"namespace y nombre coincidentes": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("team-a", "one")},
			resource: resource, namespace: "team-a", name: "one", expected: true,
		},
		"otro tipo de recurso no coincide": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("", "")},
			resource: other, namespace: "team-a", name: "one", expected: false,
		},
		"un objeto de clúster no entra en un filtro de namespace": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("team-a", "")},
			resource: resource, namespace: "", name: "one", expected: false,
		},
		"un objeto de clúster entra sin filtro de namespace": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("", "")},
			resource: resource, namespace: "", name: "one", expected: true,
		},
		"basta con que una entrada coincida": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("team-a", ""), scope("team-b", "")},
			resource: resource, namespace: "team-b", name: "one", expected: true,
		},
		"ninguna entrada coincide": {
			scopes:   []v1alpha1.AdmissionResourceGroupT{scope("team-a", ""), scope("team-c", "")},
			resource: resource, namespace: "team-b", name: "one", expected: false,
		},
		"sin entradas no cubre nada": {
			scopes:   nil,
			resource: resource, namespace: "team-a", name: "one", expected: false,
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			got := matchesScopes(testCase.scopes, testCase.resource, testCase.namespace, testCase.name)
			if got != testCase.expected {
				t.Errorf("got %v, expected %v", got, testCase.expected)
			}
		})
	}
}
