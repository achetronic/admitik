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
	"strings"
	"testing"
	"time"

	//
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	//
	"github.com/freepik-company/admitik/internal/informer"
	policyStore "github.com/freepik-company/admitik/internal/policystore"
)

// newUnreachableController builds a controller whose informers never sync, because the client
// points nowhere
func newUnreachableController(t *testing.T, sources ...string) *SourcesController {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	client, err := dynamic.NewForConfig(&rest.Config{Host: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return &SourcesController{
		Dependencies: SourcesControllerDependencies{
			Context:      &ctx,
			PolicyStores: []policyStore.SourceProvider{&testSourceProvider{sources: sources}},
			Pool:         informer.NewPool(&ctx, client, 0),
		},
	}
}

func TestReadyWaitsForTheFirstReconciliation(t *testing.T) {
	controller := newTestController(t)

	err := controller.Ready()
	if err == nil || !strings.Contains(err.Error(), readyWaitingForReconcile) {
		t.Errorf("got %v, expected the reconciliation error", err)
	}
}

func TestReadyWhenThereAreNoSources(t *testing.T) {
	controller := newTestController(t)
	controller.reconcileInformers()

	if err := controller.Ready(); err != nil {
		t.Errorf("got %v, expected the replica to be ready", err)
	}
}

func TestReadyWaitsForTheCaches(t *testing.T) {
	controller := newUnreachableController(t, "/v1/configmaps")

	controller.startedAt = time.Now()
	controller.reconcileInformers()

	err := controller.Ready()
	if err == nil || !strings.Contains(err.Error(), readyWaitingForCaches) {
		t.Errorf("got %v, expected the cache error", err)
	}
}

func TestReadyGivesUpWaitingAfterTheTimeout(t *testing.T) {
	controller := newUnreachableController(t, "/v1/configmaps")

	// A source that does not exist in the cluster never syncs, and one broken policy must not
	// take the replica out of rotation forever
	controller.startedAt = time.Now().Add(-2 * cacheSyncTimeout)
	controller.reconcileInformers()

	if err := controller.Ready(); err != nil {
		t.Errorf("got %v, expected the replica to be ready after the timeout", err)
	}
}

func TestReadyIsALatch(t *testing.T) {
	controller := newTestController(t, &testSourceProvider{sources: []string{"/v1/configmaps"}})
	controller.startedAt = time.Now()
	controller.reconcileInformers()

	// The replica becomes ready once the cache is filled
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if controller.Ready() == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := controller.Ready(); err != nil {
		t.Fatalf("got %v, expected the replica to be ready", err)
	}

	// A source added later must not take the replica out of rotation
	controller.Dependencies.PolicyStores[0].(*testSourceProvider).sources = []string{"/v1/secrets"}
	controller.startedAt = time.Now()
	controller.reconcileInformers()

	if err := controller.Ready(); err != nil {
		t.Errorf("got %v, expected the replica to stay ready", err)
	}
}
