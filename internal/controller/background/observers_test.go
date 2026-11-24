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
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	//
	"k8s.io/apimachinery/pkg/watch"
)

// spyProcessor records the events it receives
type spyProcessor struct {
	mu     sync.Mutex
	events []watch.EventType
}

func (s *spyProcessor) Process(_ context.Context, _ string, eventType watch.EventType, _ ...map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events = append(s.events, eventType)
}

func (s *spyProcessor) received() []watch.EventType {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.events
}

func TestObservers(t *testing.T) {
	registry := newObservers()

	registry.set("v1/namespaces//", []string{"clustergenerationpolicies"})

	if got := registry.get("v1/namespaces//"); !slices.Equal(got, []string{"clustergenerationpolicies"}) {
		t.Errorf("got %v, expected the registered processors", got)
	}
	if got := registry.get("v1/configmaps//"); len(got) != 0 {
		t.Errorf("got %v, expected no processors for an unknown resource", got)
	}
}

func TestObserversForget(t *testing.T) {
	registry := newObservers()
	registry.set("v1/namespaces//", []string{"a"})
	registry.set("v1/configmaps//", []string{"b"})

	registry.forget([]string{"v1/namespaces//"})

	if got := registry.get("v1/namespaces//"); len(got) == 0 {
		t.Error("the watched resource was forgotten")
	}
	if got := registry.get("v1/configmaps//"); len(got) != 0 {
		t.Error("the resource nobody watches anymore is still there")
	}
}

func TestDispatchRunsTheInterestedProcessors(t *testing.T) {
	registry := newObservers()
	processors := &spyProcessor{}
	dispatcher := NewEventDispatcher(EventDispatcherDependencies{
		Context:    context.Background(),
		Observers:  registry,
		Processors: map[string]Processor{"generation": processors},
	})

	registry.set("v1/namespaces//", []string{"generation"})
	dispatcher.Dispatch("v1/namespaces//", watch.Modified, map[string]any{"kind": "Namespace"})

	waitFor(t, processors, 1)
}

func TestDispatchSkipsTheResourcesNobodyWatches(t *testing.T) {
	registry := newObservers()
	processors := &spyProcessor{}
	dispatcher := NewEventDispatcher(EventDispatcherDependencies{
		Context:    context.Background(),
		Observers:  registry,
		Processors: map[string]Processor{"generation": processors},
	})

	dispatcher.Dispatch("v1/configmaps//", watch.Added, map[string]any{})

	if got := processors.received(); len(got) != 0 {
		t.Errorf("got %v, expected no events", got)
	}
}

func TestDispatchIgnoresUnknownProcessors(t *testing.T) {
	registry := newObservers()
	registry.set("v1/namespaces//", []string{"missing"})

	dispatcher := NewEventDispatcher(EventDispatcherDependencies{
		Context:    context.Background(),
		Observers:  registry,
		Processors: map[string]Processor{},
	})

	// It must not panic
	dispatcher.Dispatch("v1/namespaces//", watch.Added, map[string]any{})
}

func waitFor(t *testing.T, processor *spyProcessor, count int) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(processor.received()) >= count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("got %v events, expected %d", processor.received(), count)
}
