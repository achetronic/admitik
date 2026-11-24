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
	"sync"
	"testing"
	"time"

	//
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicFake "k8s.io/client-go/dynamic/fake"
)

var testGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

func newTestClient(t *testing.T, objects ...runtime.Object) *dynamicFake.FakeDynamicClient {
	t.Helper()

	listKinds := map[schema.GroupVersionResource]string{testGVR: "ConfigMapList"}
	return dynamicFake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
}

func newTestInformer(t *testing.T, client *dynamicFake.FakeDynamicClient) (*Informer, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	inform, err := NewInformer(
		Options{GVR: testGVR},
		Dependencies{Context: &ctx, Client: client},
	)
	if err != nil {
		cancel()
		t.Fatalf("NewInformer returned an error: %v", err)
	}

	return inform, cancel
}

func TestNewInformerValidatesItsDependencies(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name string
		deps Dependencies
	}{
		{name: "client is required", deps: Dependencies{Context: &ctx}},
		{name: "context is required", deps: Dependencies{Client: newTestClient(t)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inform, err := NewInformer(Options{GVR: testGVR}, test.deps)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if inform != nil {
				t.Errorf("expected a nil informer, got %v", inform)
			}
		})
	}
}

func TestStopIsIdempotent(t *testing.T) {
	inform, cancel := newTestInformer(t, newTestClient(t))
	defer cancel()

	inform.Start()

	// Destroying a source informer and shutting down the controller concurrently
	// must not close the stop channel twice
	inform.Stop()
	inform.Stop()
}

func TestStartIsIdempotent(t *testing.T) {
	inform, cancel := newTestInformer(t, newTestClient(t))
	defer cancel()

	inform.Start()
	inform.Start()

	if !inform.IsStarted() {
		t.Error("informer is not started")
	}
}

func TestEventHandlerReceivesCachedObjects(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "existing", "namespace": "default"},
	}}

	inform, cancel := newTestInformer(t, newTestClient(t, existing))
	defer cancel()

	var mutex sync.Mutex
	received := map[string]watch.EventType{}
	gotEvent := make(chan struct{}, 1)

	err := inform.WithEventHandlerFunc(func(eventType watch.EventType, obj *unstructured.Unstructured, oldObj *unstructured.Unstructured) error {
		mutex.Lock()
		received[obj.GetName()] = eventType
		mutex.Unlock()

		select {
		case gotEvent <- struct{}{}:
		default:
		}

		return nil
	})
	if err != nil {
		t.Fatalf("WithEventHandlerFunc returned an error: %v", err)
	}

	inform.Start()

	select {
	case <-gotEvent:
	case <-time.After(5 * time.Second):
		t.Fatal("no event received for the cached object")
	}

	mutex.Lock()
	defer mutex.Unlock()
	if received["existing"] != watch.Added {
		t.Errorf("got event %q for 'existing', expected %q", received["existing"], watch.Added)
	}
}

func TestEventHandlerCannotBeAddedAfterStart(t *testing.T) {
	inform, cancel := newTestInformer(t, newTestClient(t))
	defer cancel()

	inform.Start()

	err := inform.WithEventHandlerFunc(func(
		watch.EventType, *unstructured.Unstructured, *unstructured.Unstructured) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

func TestGetStoreReturnsCachedObjects(t *testing.T) {
	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "existing", "namespace": "default"},
	}}

	inform, cancel := newTestInformer(t, newTestClient(t, existing))
	defer cancel()

	gotEvent := make(chan struct{}, 1)
	if err := inform.WithEventHandlerFunc(func(
		watch.EventType, *unstructured.Unstructured, *unstructured.Unstructured) error {
		select {
		case gotEvent <- struct{}{}:
		default:
		}
		return nil
	}); err != nil {
		t.Fatalf("WithEventHandlerFunc returned an error: %v", err)
	}

	inform.Start()

	select {
	case <-gotEvent:
	case <-time.After(5 * time.Second):
		t.Fatal("no event received for the cached object")
	}

	list := inform.GetStore().List()
	if len(list) != 1 {
		t.Fatalf("got %d cached objects, expected 1", len(list))
	}

	object, isUnstructured := list[0].(*unstructured.Unstructured)
	if !isUnstructured {
		t.Fatalf("cached object is not *unstructured.Unstructured: %T", list[0])
	}
	if object.GetName() != "existing" {
		t.Errorf("got cached object %q, expected 'existing'", object.GetName())
	}
}

func TestStoppingInformersThroughTheRegistry(t *testing.T) {
	// Stopping through the registry and through the context at the same time must be safe
	inform, cancel := newTestInformer(t, newTestClient(t))
	inform.Start()

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		inform.Stop()
	}()
	go func() {
		defer waitGroup.Done()
		cancel()
	}()
	waitGroup.Wait()
}
