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

package policystore

import (
	"slices"
	"strings"

	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//
	"github.com/freepik-company/admitik/internal/store/simple"
)

func NewPolicyStore[T PolicyResourceI]() *PolicyStore[T] {

	return &PolicyStore[T]{
		Store: simple.NewStore[T](),
	}
}

// GetReferencedSources returns a list of GVR names referenced on 'sources' section across the policies.
// GVR is expressed as {group}/{version}/{resource}
func (ps *PolicyStore[T]) GetReferencedSources() []string {

	sourceTypes := []string{}

	// Loop over all objects collecting extra resources
	for _, collectionName := range ps.Store.GetCollectionNames() {
		for _, resourceObj := range ps.Store.GetResources(collectionName) {
			for _, source := range resourceObj.GetSources() {

				// Prevents potential explosions due to
				// 'sources' comes empty from time to time
				if source.GroupVersionResource == (metav1.GroupVersionResource{}) {
					continue
				}

				sourceTypes = append(sourceTypes, strings.Join([]string{source.Group, source.Version, source.Resource}, "/"))
			}
		}
	}

	// Clean duplicated
	slices.Sort(sourceTypes)
	return slices.Compact(sourceTypes)
}
