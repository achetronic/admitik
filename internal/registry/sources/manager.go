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
	"k8s.io/apimachinery/pkg/runtime/schema"

	//
	"github.com/freepik-company/admitik/internal/informer"
)

// NewSourcesRegistry creates a registry of informers, one per watched GVR
func NewSourcesRegistry() *SourcesRegistry {

	return &SourcesRegistry{
		informers: make(map[schema.GroupVersionResource]*informer.Informer),
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
// Launching it is up to the controller using this registry
func (m *SourcesRegistry) RegisterInformer(gvr schema.GroupVersionResource, inf *informer.Informer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.informers[gvr] = inf
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
