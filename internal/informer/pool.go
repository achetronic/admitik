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

package informer

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	//
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
)

// Scope is the portion of a resource type a consumer wants to watch.
// An empty Namespace or Name means every namespace or name
type Scope struct {
	GVR       schema.GroupVersionResource
	Namespace string
	Name      string
}

// String returns the scope as {group}/{version}/{resource}/{namespace}/{name}
func (s Scope) String() string {
	return fmt.Sprintf("%s/%s/%s/%s/%s", s.GVR.Group, s.GVR.Version, s.GVR.Resource, s.Namespace, s.Name)
}

func (s Scope) options(resync time.Duration) Options {

	options := Options{
		GVR:                      s.GVR,
		Namespace:                s.Namespace,
		InformerDurationToResync: resync,
	}

	if s.Name != "" {
		options.FieldSelector = "metadata.name=" + s.Name
	}

	return options
}

// Snapshot is the cached content of a scope at a given version.
// Two snapshots with the same version hold the same objects
type Snapshot struct {
	Objects []*map[string]any
	Version uint64
}

// Pool keeps one informer per Scope and shares it between the consumers that ask for it.
// Every consumer owns its subscription, so a scope is only dropped when nobody needs it
type Pool struct {
	context *context.Context
	client  dynamic.Interface
	resync  time.Duration

	mu        sync.RWMutex
	informers map[Scope]*Informer

	// versions is increased every time an informer reports a change, so consumers can tell
	// whether the cached objects are the same ones they saw before
	versions map[Scope]uint64

	// subscribers holds, for each scope, the consumers watching it and the handlers they want
	subscribers map[Scope]map[string][]EventHandlerFunc
}

func NewPool(ctx *context.Context, client dynamic.Interface, resync time.Duration) *Pool {

	return &Pool{
		context:     ctx,
		client:      client,
		resync:      resync,
		informers:   make(map[Scope]*Informer),
		versions:    make(map[Scope]uint64),
		subscribers: make(map[Scope]map[string][]EventHandlerFunc),
	}
}

// Ensure returns the informer for the scope, creating and starting it when missing.
// The consumer is subscribed to the scope, so the informer lives until every consumer releases it
func (p *Pool) Ensure(consumer string, scope Scope, handlers ...EventHandlerFunc) (*Informer, error) {

	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, found := p.informers[scope]; found {
		p.subscribe(consumer, scope, handlers)
		return existing, nil
	}

	inform, err := NewInformer(scope.options(p.resync), Dependencies{Context: p.context, Client: p.client})
	if err != nil {
		return nil, fmt.Errorf("error creating an informer for %s: %w", scope, err)
	}

	if err := inform.WithEventHandlerFunc(p.dispatch(scope)); err != nil {
		return nil, err
	}

	p.informers[scope] = inform
	p.subscribe(consumer, scope, handlers)
	inform.Start()

	return inform, nil
}

// Get returns the informer of a scope, or nil when there is none
func (p *Pool) Get(scope Scope) *Informer {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.informers[scope]
}

// Snapshot returns the cached objects of a scope along with the version they belong to
func (p *Pool) Snapshot(scope Scope) Snapshot {

	p.mu.RLock()
	inform, found := p.informers[scope]
	version := p.versions[scope]
	p.mu.RUnlock()

	if !found || inform == nil {
		return Snapshot{Objects: []*map[string]any{}, Version: version}
	}

	cached := inform.GetStore().List()

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

// Owned returns the scopes the consumer is subscribed to
func (p *Pool) Owned(consumer string) []Scope {
	p.mu.RLock()
	defer p.mu.RUnlock()

	scopes := []Scope{}
	for scope, subscribers := range p.subscribers {
		if _, subscribed := subscribers[consumer]; subscribed {
			scopes = append(scopes, scope)
		}
	}

	return scopes
}

// Release removes the consumer from the scope. The informer is stopped when nobody else needs it
func (p *Pool) Release(consumer string, scope Scope) {
	p.mu.Lock()
	defer p.mu.Unlock()

	subscribers, found := p.subscribers[scope]
	if !found {
		return
	}

	delete(subscribers, consumer)
	if len(subscribers) > 0 {
		return
	}

	if inform, exists := p.informers[scope]; exists {
		if inform != nil {
			inform.Stop()
		}
	}

	delete(p.subscribers, scope)
	delete(p.informers, scope)
	delete(p.versions, scope)
}

// dispatch returns the handler registered in the informer. It keeps the version of the scope and
// fans out to every consumer subscribed to it
func (p *Pool) dispatch(scope Scope) EventHandlerFunc {

	return func(eventType watch.EventType, obj, oldObj *unstructured.Unstructured) error {

		p.mu.Lock()
		p.versions[scope]++

		handlers := []EventHandlerFunc{}
		for _, consumerHandlers := range p.subscribers[scope] {
			handlers = append(handlers, consumerHandlers...)
		}
		p.mu.Unlock()

		for _, handler := range handlers {
			if err := handler(eventType, obj, oldObj); err != nil {
				return err
			}
		}

		return nil
	}
}

// subscribe registers the handlers of a consumer for a scope. It must be called with the lock held
func (p *Pool) subscribe(consumer string, scope Scope, handlers []EventHandlerFunc) {

	if p.subscribers[scope] == nil {
		p.subscribers[scope] = map[string][]EventHandlerFunc{}
	}

	p.subscribers[scope][consumer] = slices.Clone(handlers)
}
