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
	"fmt"

	//
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
)

// fieldManager is the server side apply manager that owns the fields of the generated objects
const fieldManager = "admitik"

// Writer creates or updates the objects a policy renders
type Writer struct {
	client dynamic.Interface
	mapper meta.RESTMapper
}

func NewWriter(client dynamic.Interface, mapper meta.RESTMapper) *Writer {
	return &Writer{client: client, mapper: mapper}
}

// Options describe who is writing the object and how
type Options struct {
	// PolicyName is the policy writing the object
	PolicyName string

	// PolicyKind is the kind of that policy
	PolicyKind string

	// Overwrite takes over an object that already exists instead of leaving it alone
	Overwrite bool
}

// Result is what happened with the object
type Result struct {
	Resource  string
	Namespace string
	Name      string

	// Created says whether the object was written now or taken over
	Created bool

	// Existing is true when the object was already there and overwriting was not allowed
	Existing bool
}

// Apply writes the object, resolving its resource type and marking it as written by the policy
func (w *Writer) Apply(ctx context.Context, options Options, object map[string]any) (Result, error) {

	converted := &unstructured.Unstructured{Object: object}
	gvk := converted.GroupVersionKind()

	mapping, err := w.mapper.RESTMapping(
		schema.GroupKind{Group: gvk.Group, Kind: gvk.Kind}, gvk.Version)
	if err != nil {
		return Result{}, fmt.Errorf("error resolving the resource of %s: %w", gvk, err)
	}

	result := Result{
		Resource:  mapping.Resource.Resource,
		Namespace: converted.GetNamespace(),
		Name:      converted.GetName(),
	}

	markProvenance(converted, options)
	resourceClient := w.client.Resource(mapping.Resource).Namespace(result.Namespace)

	_, err = resourceClient.Create(ctx, converted, metav1.CreateOptions{})
	if err == nil {
		result.Created = true
		return result, nil
	}

	if !apierrors.IsAlreadyExists(err) {
		return result, fmt.Errorf("error creating %s/%s: %w", result.Resource, result.Name, err)
	}

	if !options.Overwrite {
		result.Existing = true
		return result, nil
	}

	_, err = resourceClient.Apply(ctx, result.Name, converted, metav1.ApplyOptions{
		FieldManager: fieldManager,
		Force:        true,
	})
	if err != nil {
		return result, fmt.Errorf("error updating %s/%s: %w", result.Resource, result.Name, err)
	}

	return result, nil
}

// markProvenance writes on the object which policy wrote it, so it can be told apart from the
// objects the users write
func markProvenance(object *unstructured.Unstructured, options Options) {

	labels := object.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}

	labels[v1alpha1.ManagedByLabel] = v1alpha1.ManagedByValue
	labels[v1alpha1.PolicyLabel] = options.PolicyName
	labels[v1alpha1.PolicyKindLabel] = options.PolicyKind

	object.SetLabels(labels)
}
