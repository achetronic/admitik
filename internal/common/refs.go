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

package common

import (
	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// ObjectRef identifies an object in the logs: what it is, where it lives and its name
type ObjectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

// ObjectRefOf builds the reference of an object from its type and its identity
func ObjectRefOf(gvk schema.GroupVersionKind, namespace, name string) ObjectRef {

	apiVersion := gvk.Version
	if gvk.Group != "" {
		apiVersion = gvk.Group + "/" + gvk.Version
	}

	return ObjectRef{APIVersion: apiVersion, Kind: gvk.Kind, Namespace: namespace, Name: name}
}

// ObjectRefOfAdmission builds the reference of the object an admission request is about
func ObjectRefOfAdmission(kind metav1.GroupVersionKind, namespace, name string) ObjectRef {

	return ObjectRefOf(schema.GroupVersionKind{
		Group: kind.Group, Version: kind.Version, Kind: kind.Kind,
	}, namespace, name)
}

// ObjectRefOfObject builds the reference of an object that was already decoded
func ObjectRefOfObject(object map[string]any) ObjectRef {

	apiVersion, _ := object["apiVersion"].(string)
	kind, _ := object["kind"].(string)

	namespace, name := "", ""
	if metadata, isMap := object["metadata"].(map[string]any); isMap {
		namespace, _ = metadata["namespace"].(string)
		name, _ = metadata["name"].(string)
	}

	return ObjectRef{APIVersion: apiVersion, Kind: kind, Namespace: namespace, Name: name}
}
