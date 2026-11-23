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

var poolGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

func newTestPool(t *testing.T, objects ...runtime.Object) *Pool {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	listKinds := map[schema.GroupVersionResource]string{poolGVR: "ConfigMapList"}
	client := dynamicFake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)

	return NewPool(&ctx, client, 0)
}

func testObject(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": name, "namespace": "default"},
	}}
}

func TestEnsureReturnsTheSameInformerPerScope(t *testing.T) {
	pool := newTestPool(t)
	scope := Scope{GVR: poolGVR}

	first, err := pool.Ensure("consumer-a", scope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	second, err := pool.Ensure("consumer-b", scope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if first != second {
		t.Error("the scope was watched twice")
	}
	if pool.Get(scope) != first {
		t.Error("Get returned another informer")
	}
}

func TestEnsureDeliversEventsToEveryConsumer(t *testing.T) {
	pool := newTestPool(t, testObject("existing"))
	scope := Scope{GVR: poolGVR}

	var mutex sync.Mutex
	received := map[string]int{}

	handlerFor := func(consumer string) EventHandlerFunc {
		return func(watch.EventType, *unstructured.Unstructured, *unstructured.Unstructured) error {
			mutex.Lock()
			defer mutex.Unlock()

			received[consumer]++
			return nil
		}
	}

	if _, err := pool.Ensure("consumer-a", scope, handlerFor("consumer-a")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := pool.Ensure("consumer-b", scope, handlerFor("consumer-b")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mutex.Lock()
		done := received["consumer-a"] > 0 && received["consumer-b"] > 0
		mutex.Unlock()

		if done {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	mutex.Lock()
	defer mutex.Unlock()
	t.Fatalf("both consumers should have received the event, got %v", received)
}

func TestEnsureReplacesTheHandlersOfTheSameConsumer(t *testing.T) {
	pool := newTestPool(t, testObject("existing"))
	scope := Scope{GVR: poolGVR}

	var mutex sync.Mutex
	calls := 0

	handler := func(watch.EventType, *unstructured.Unstructured, *unstructured.Unstructured) error {
		mutex.Lock()
		defer mutex.Unlock()

		calls++
		return nil
	}

	// The controller ensures the same scope on every reconcile, so this must not duplicate
	for run := 0; run < 3; run++ {
		if _, err := pool.Ensure("consumer-a", scope, handler); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mutex.Lock()
		count := calls
		mutex.Unlock()

		if count > 0 {
			time.Sleep(50 * time.Millisecond)

			mutex.Lock()
			defer mutex.Unlock()
			if calls != 1 {
				t.Errorf("the handler was called %d times for one object, expected 1", calls)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("no event received")
}

func TestReleaseKeepsTheInformerWhileSomeoneUsesIt(t *testing.T) {
	pool := newTestPool(t)
	scope := Scope{GVR: poolGVR}

	if _, err := pool.Ensure("consumer-a", scope); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inform, err := pool.Ensure("consumer-b", scope)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pool.Release("consumer-a", scope)

	if pool.Get(scope) != inform {
		t.Error("the informer was dropped although another consumer was using it")
	}
	if owned := pool.Owned("consumer-a"); len(owned) != 0 {
		t.Errorf("got %v, expected no scopes for the released consumer", owned)
	}
	if owned := pool.Owned("consumer-b"); len(owned) != 1 {
		t.Errorf("got %v, expected one scope for the remaining consumer", owned)
	}
}

func TestReleaseDropsTheInformerWhenNobodyUsesIt(t *testing.T) {
	pool := newTestPool(t)
	scope := Scope{GVR: poolGVR}

	if _, err := pool.Ensure("consumer-a", scope); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pool.Release("consumer-a", scope)

	if pool.Get(scope) != nil {
		t.Error("the informer is still in the pool")
	}
	if owned := pool.Owned("consumer-a"); len(owned) != 0 {
		t.Errorf("got %v, expected no scopes", owned)
	}
}

func TestReleaseOfAnUnknownScopeDoesNothing(t *testing.T) {
	pool := newTestPool(t)

	pool.Release("consumer-a", Scope{GVR: poolGVR})
}

func TestSnapshotFollowsTheInformerEvents(t *testing.T) {
	pool := newTestPool(t, testObject("existing"), testObject("another"))
	scope := Scope{GVR: poolGVR}

	if _, err := pool.Ensure("consumer-a", scope); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := pool.Snapshot(scope)
		if snapshot.Version > 0 && len(snapshot.Objects) == 2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	snapshot := pool.Snapshot(scope)
	t.Fatalf("got version %d with %d objects, expected two cached objects", snapshot.Version, len(snapshot.Objects))
}

func TestSnapshotOfAnUnknownScope(t *testing.T) {
	pool := newTestPool(t)

	snapshot := pool.Snapshot(Scope{GVR: poolGVR})
	if snapshot.Objects == nil {
		t.Error("got nil objects, expected an empty list")
	}
	if len(snapshot.Objects) != 0 {
		t.Errorf("got %d objects, expected none", len(snapshot.Objects))
	}
}

func TestScopeString(t *testing.T) {
	scope := Scope{
		GVR:       schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"},
		Namespace: "default",
		Name:      "demo",
	}

	if got, expected := scope.String(), "apps/v1/deployments/default/demo"; got != expected {
		t.Errorf("got %q, expected %q", got, expected)
	}
}

func TestPoolIsSyncedWithoutInformers(t *testing.T) {
	pool := newTestPool(t)

	if !pool.Synced() {
		t.Error("an empty pool should be considered synced")
	}
}

func TestPoolIsSyncedOnlyWhenEveryInformerHasItsCache(t *testing.T) {
	pool := newTestPool(t, testObject("existing"))
	scope := Scope{GVR: poolGVR}

	if _, err := pool.Ensure("consumer-a", scope); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pool.Synced() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("the pool never reported its informers as synced")
}
