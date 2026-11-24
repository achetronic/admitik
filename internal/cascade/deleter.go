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
	"errors"
	"fmt"
	"slices"
	"strings"

	//
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
)

const (
	// pageSize is how many objects are requested at a time
	pageSize = 500

	// ignoredResource is left out of the sweep: there are thousands of them and they come and go
	ignoredResource = "events"
)

// Deleter removes the objects a policy wrote, looking for its label in every resource type of the
// cluster. There is no way to list across resource types, so it asks discovery what exists
type Deleter struct {
	client    dynamic.Interface
	discovery discovery.DiscoveryInterface
}

func New(client dynamic.Interface, discoveryClient discovery.DiscoveryInterface) *Deleter {
	return &Deleter{client: client, discovery: discoveryClient}
}

// Delete removes every object labelled with the policy and returns how many were deleted.
// Objects that disappear while the sweep runs are not an error
func (d *Deleter) Delete(ctx context.Context, policyKind, policyName string) (int, error) {

	resources, resourcesErr := d.resourceTypes()

	deleted := 0
	var deleteErrors []error

	for _, resource := range resources {

		gvr := resource.GroupVersionResource
		objects, err := d.list(ctx, gvr, selectorFor(policyKind, policyName))
		if err != nil {
			deleteErrors = append(deleteErrors, err)
			continue
		}

		for _, obj := range objects {

			var resourceClient dynamic.ResourceInterface = d.client.Resource(gvr)
			if resource.Namespaced {
				resourceClient = d.client.Resource(gvr).Namespace(namespaceOf(obj))
			}

			err := resourceClient.Delete(ctx, nameOf(obj), metav1.DeleteOptions{})
			if apierrors.IsNotFound(err) {
				continue
			}
			if err != nil {
				deleteErrors = append(deleteErrors, fmt.Errorf("error deleting %s/%s: %w", gvr, nameOf(obj), err))
				continue
			}

			deleted++
		}
	}

	return deleted, errors.Join(append(deleteErrors, resourcesErr)...)
}

// resourceType is a resource type the sweep has to look at
type resourceType struct {
	schema.GroupVersionResource
	Namespaced bool
}

// resourceTypes returns the resource types that can hold objects written by a policy
func (d *Deleter) resourceTypes() ([]resourceType, error) {

	_, resourceLists, err := d.discovery.ServerGroupsAndResources()

	resources := []resourceType{}
	for _, resourceList := range resourceLists {

		group, version := groupAndVersion(resourceList.GroupVersion)

		for _, resource := range resourceList.APIResources {

			// Subresources, events and the types that cannot be listed or deleted are not worth a call
			if strings.Contains(resource.Name, "/") {
				continue
			}
			if resource.Name == ignoredResource {
				continue
			}
			if !slices.Contains(resource.Verbs, "list") || !slices.Contains(resource.Verbs, "delete") {
				continue
			}

			resources = append(resources, resourceType{
				GroupVersionResource: schema.GroupVersionResource{
					Group:    group,
					Version:  version,
					Resource: resource.Name,
				},
				Namespaced: resource.Namespaced,
			})
		}
	}

	return resources, err
}

// list returns every object of a resource type carrying the selector
func (d *Deleter) list(ctx context.Context, gvr schema.GroupVersionResource, selector string) ([]map[string]any, error) {

	cached := []map[string]any{}
	options := metav1.ListOptions{LabelSelector: selector, Limit: pageSize}

	for {
		list, err := d.client.Resource(gvr).List(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("error listing %s: %w", gvr, err)
		}

		for index := range list.Items {
			cached = append(cached, list.Items[index].Object)
		}

		if list.GetContinue() == "" {
			return cached, nil
		}

		options.Continue = list.GetContinue()
	}
}

// selectorFor returns the label selector that matches what a policy wrote
func selectorFor(policyKind, policyName string) string {

	return strings.Join([]string{
		v1alpha1.ManagedByLabel + "=" + v1alpha1.ManagedByValue,
		v1alpha1.PolicyLabel + "=" + policyName,
		v1alpha1.PolicyKindLabel + "=" + policyKind,
	}, ",")
}

// groupAndVersion splits a discovery group version, where the core group comes without a group
func groupAndVersion(groupVersion string) (group, version string) {

	parts := strings.Split(groupVersion, "/")
	if len(parts) == 1 {
		return "", parts[0]
	}

	return parts[0], parts[1]
}

// nameOf returns the name of an object
func nameOf(object map[string]any) string {
	metadata, isMap := object["metadata"].(map[string]any)
	if !isMap {
		return ""
	}

	name, _ := metadata["name"].(string)
	return name
}

// namespaceOf returns the namespace of an object, empty when it has none
func namespaceOf(object map[string]any) string {
	metadata, isMap := object["metadata"].(map[string]any)
	if !isMap {
		return ""
	}

	namespace, _ := metadata["namespace"].(string)
	return namespace
}
