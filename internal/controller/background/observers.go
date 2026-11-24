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

package background

import (
	"slices"
	"sync"
)

// observers keeps which processors have to react to the events of each watched resource.
// Several kinds of policy can watch the same resource type, so the events of one resource are
// dispatched to a list of processors.
//
// The key is the watched resource as {group}/{version}/{resource}/{namespace}/{name}, the same key
// the controllers use in the policy store. Example:
//
//	observers.set("/v1/namespaces//", []string{"clustergenerationpolicies"})
//	observers.get("/v1/namespaces//") // ["clustergenerationpolicies"]
type observers struct {
	mu sync.RWMutex

	byResource map[string][]string
}

func newObservers() *observers {
	return &observers{byResource: map[string][]string{}}
}

// set replaces the processors that want the events of a watched resource
func (o *observers) set(resource string, processors []string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.byResource[resource] = processors
}

// get returns the processors that want the events of a watched resource
func (o *observers) get(resource string) []string {
	o.mu.RLock()
	defer o.mu.RUnlock()

	return o.byResource[resource]
}

// forget removes every entry that is not in the provided list
func (o *observers) forget(watched []string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for resource := range o.byResource {
		if slices.Contains(watched, resource) {
			continue
		}

		delete(o.byResource, resource)
	}
}
