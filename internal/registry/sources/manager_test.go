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
	"context"
	"testing"
	"time"

	//
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicFake "k8s.io/client-go/dynamic/fake"

	//
	"github.com/freepik-company/admitik/internal/informer"
)

var testGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

func newTestInformer(t *testing.T) *informer.Informer {
	t.Helper()

	ctx := context.Background()
	inform, err := informer.NewInformer(
		informer.Options{GVR: testGVR},
		informer.Dependencies{
			Context: &ctx,
			Client: dynamicFake.NewSimpleDynamicClientWithCustomListKinds(
				runtime.NewScheme(),
				map[schema.GroupVersionResource]string{testGVR: "ConfigMapList"},
			),
		},
	)
	if err != nil {
		t.Fatalf("NewInformer returned an error: %v", err)
	}

	return inform
}

func TestSnapshotOfAnUnknownResource(t *testing.T) {
	registry := NewSourcesRegistry()

	snapshot := registry.Snapshot(testGVR)
	if snapshot.Objects == nil {
		t.Error("got nil objects, expected an empty list")
	}
	if len(snapshot.Objects) != 0 {
		t.Errorf("got %d objects, expected none", len(snapshot.Objects))
	}
}

func TestRegisteredInformerIsReturned(t *testing.T) {
	registry := NewSourcesRegistry()
	inform := newTestInformer(t)

	if err := registry.RegisterInformer(testGVR, inform); err != nil {
		t.Fatalf("RegisterInformer returned an error: %v", err)
	}

	if !registry.InformerIsRegistered(testGVR) {
		t.Error("the informer is not registered")
	}
	if registry.GetInformer(testGVR) != inform {
		t.Error("GetInformer returned another informer")
	}
	if types := registry.GetRegisteredResourceTypes(); len(types) != 1 {
		t.Errorf("got %v, expected one registered type", types)
	}
}

func TestDestroyedInformerIsForgotten(t *testing.T) {
	registry := NewSourcesRegistry()
	inform := newTestInformer(t)

	if err := registry.RegisterInformer(testGVR, inform); err != nil {
		t.Fatalf("RegisterInformer returned an error: %v", err)
	}

	registry.DestroyInformer(testGVR)

	if registry.InformerIsRegistered(testGVR) {
		t.Error("the informer is still registered")
	}
	if registry.GetInformer(testGVR) != nil {
		t.Error("GetInformer returned an informer for a destroyed type")
	}
	if types := registry.GetRegisteredResourceTypes(); len(types) != 0 {
		t.Errorf("got %v, expected no registered types", types)
	}
}

func TestSnapshotVersionStartsAtZero(t *testing.T) {
	registry := NewSourcesRegistry()
	inform := newTestInformer(t)

	if err := registry.RegisterInformer(testGVR, inform); err != nil {
		t.Fatalf("RegisterInformer returned an error: %v", err)
	}

	if version := registry.Snapshot(testGVR).Version; version != 0 {
		t.Errorf("got version %d, expected 0", version)
	}
}

func TestSnapshotVersionFollowsTheInformerEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	existing := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "existing", "namespace": "default"},
	}}

	inform, err := informer.NewInformer(
		informer.Options{GVR: testGVR},
		informer.Dependencies{
			Context: &ctx,
			Client: dynamicFake.NewSimpleDynamicClientWithCustomListKinds(
				runtime.NewScheme(),
				map[schema.GroupVersionResource]string{testGVR: "ConfigMapList"},
				existing,
			),
		},
	)
	if err != nil {
		t.Fatalf("NewInformer returned an error: %v", err)
	}

	registry := NewSourcesRegistry()
	if err := registry.RegisterInformer(testGVR, inform); err != nil {
		t.Fatalf("RegisterInformer returned an error: %v", err)
	}

	inform.Start()

	// The initial list produces an event for the existing object, which must bump the version
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if registry.Snapshot(testGVR).Version > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("the version did not change after the informer reported an object")
}
