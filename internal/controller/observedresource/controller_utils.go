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
	"strings"
	"sync"

	//
	"k8s.io/apimachinery/pkg/runtime/schema"

	//
	"github.com/freepik-company/admitik/internal/globals"
)

// GVKR represents TODO
type GVKR struct {
	GVK         schema.GroupVersionKind
	Resource    string
	Subresource string

	//
	Namespaced bool
}

// availableResources keeps the resource types the cluster supports. It is refreshed in background
// and read by the processors, so it needs a lock
type availableResources struct {
	mu   sync.RWMutex
	list []GVKR
}

func (a *availableResources) replace(list []GVKR) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.list = list
}

func (a *availableResources) get() []GVKR {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.list
}

// fetchKubeAvailableResources returns the resource types the cluster supports
func fetchKubeAvailableResources() (resources []GVKR, err error) {

	resources = []GVKR{}

	_, apiGroupResourcesLists, err := globals.Application.KubeDiscoveryClient.ServerGroupsAndResources()
	if err != nil {
		return resources, err
	}

	for _, apiGroupResourcesList := range apiGroupResourcesLists {

		var tmpGroup, tmpVersion string
		groupVersionParts := strings.Split(apiGroupResourcesList.GroupVersion, "/")
		tmpVersion = groupVersionParts[0]
		if len(groupVersionParts) > 1 {
			tmpGroup = groupVersionParts[0]
			tmpVersion = groupVersionParts[1]
		}

		for _, apiGroupResource := range apiGroupResourcesList.APIResources {

			tmpObj := &GVKR{}

			tmpObj.Resource = apiGroupResource.Name
			resourceParts := strings.Split(tmpObj.Resource, "/")
			if len(resourceParts) > 1 {
				tmpObj.Resource = resourceParts[0]
				tmpObj.Subresource = strings.Join(resourceParts[1:], "/")
			}

			tmpObj.GVK.Group = tmpGroup
			tmpObj.GVK.Version = tmpVersion
			tmpObj.GVK.Kind = apiGroupResource.Kind

			tmpObj.Namespaced = apiGroupResource.Namespaced

			resources = append(resources, *tmpObj)
		}
	}

	return resources, nil
}

// getResourceFromGvk returns the resource name of a kind, empty when the cluster does not have it
func getResourceFromGvk(resources *availableResources, gvk schema.GroupVersionKind) string {

	for _, object := range resources.get() {
		if object.GVK == gvk {
			return object.Resource
		}
	}
	return ""
}
