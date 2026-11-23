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

package template

// InjectedData is the context handed to the template engines.
// It is the same for every engine: what changes is how each one reads it.
type InjectedData struct {
	// Operation is the normalized event type: CREATE, UPDATE or DELETE
	Operation string

	// Object is the resource the event refers to
	Object map[string]any

	// OldObject is the previous state of the resource, only on updates
	OldObject map[string]any

	// Sources holds the extra resources requested by the policy, indexed by their position
	// in the policy 'sources' section
	Sources map[int][]map[string]any

	// Vars holds values computed by the user inside a template. Engines may read and write it
	Vars map[string]any
}

func NewInjectedData() *InjectedData {
	return &InjectedData{
		Object:    map[string]any{},
		OldObject: map[string]any{},
		Sources:   map[int][]map[string]any{},
		Vars:      map[string]any{},
	}
}

// ToMap returns the values exposed to the engines as top level names
func (d *InjectedData) ToMap() map[string]any {
	return map[string]any{
		"operation": d.Operation,
		"object":    d.Object,
		"oldObject": d.OldObject,
		"sources":   d.Sources,
		"vars":      d.Vars,
	}
}
