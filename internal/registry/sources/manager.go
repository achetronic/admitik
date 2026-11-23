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

package sources

import (
	"fmt"

	//
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"

	//
	"github.com/freepik-company/admitik/internal/informer"
)

// NewSourcesRegistry creates a registry of informers, one per watched GVR
func NewSourcesRegistry() *SourcesRegistry {

	return &SourcesRegistry{
		informers: make(map[schema.GroupVersionResource]*informer.Informer),
		versions:  make(map[schema.GroupVersionResource]uint64),
	}
}

// InformerIsRegistered returns whether an informer exists for the provided GVR
func (m *SourcesRegistry) InformerIsRegistered(gvr schema.GroupVersionResource) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, informerExists := m.informers[gvr]
	return informerExists
}

// RegisterInformer stores an already built informer for the provided GVR.
// It also hooks on its events to keep track of the version of the cached objects.
// Launching the informer is up to the controller using this registry
func (m *SourcesRegistry) RegisterInformer(gvr schema.GroupVersionResource, inf *informer.Informer) error {

	m.mu.Lock()
	m.informers[gvr] = inf
	m.mu.Unlock()

	err := inf.WithEventHandlerFunc(func(watch.EventType, *unstructured.Unstructured, *unstructured.Unstructured) error {
		m.mu.Lock()
		defer m.mu.Unlock()

		m.versions[gvr]++
		return nil
	})
	if err != nil {
		return fmt.Errorf("error hooking on the events of the informer for %v: %w", gvr, err)
	}

	return nil
}

// DestroyInformer sends a signal to the informer to stop and deletes it from the registry
func (m *SourcesRegistry) DestroyInformer(gvr schema.GroupVersionResource) {
	m.mu.Lock()
	defer m.mu.Unlock()

	informerObject, informerExists := m.informers[gvr]
	if !informerExists {
		return
	}

	if informerObject != nil {
		informerObject.Stop()
	}

	delete(m.informers, gvr)
	delete(m.versions, gvr)
}

// GetInformer returns the informer registered for the provided GVR, or nil when there is none
func (m *SourcesRegistry) GetInformer(gvr schema.GroupVersionResource) *informer.Informer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	informerObject, informerExists := m.informers[gvr]
	if !informerExists {
		return nil
	}

	return informerObject
}

// Snapshot returns the cached objects of the provided GVR along with the version they belong to
func (m *SourcesRegistry) Snapshot(gvr schema.GroupVersionResource) Snapshot {

	m.mu.RLock()
	informerObject, informerFound := m.informers[gvr]
	version := m.versions[gvr]
	m.mu.RUnlock()

	if !informerFound || informerObject == nil {
		return Snapshot{Objects: []*map[string]any{}, Version: version}
	}

	cached := informerObject.GetStore().List()

	objects := make([]*map[string]any, 0, len(cached))
	for _, item := range cached {
		object, isUnstructured := item.(*unstructured.Unstructured)
		if !isUnstructured {
			continue
		}

		objects = append(objects, &object.Object)
	}

	return Snapshot{Objects: objects, Version: version}
}
