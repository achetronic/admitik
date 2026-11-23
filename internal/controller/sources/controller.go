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
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	//
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	//
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/informer"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
)

const (
	// secondsToReconcileInformersAgain is the number of seconds to wait
	// between the moment of launching informers, and repeating this process
	// (avoid the spam, mate)
	secondsToReconcileInformersAgain = 2 * time.Second

	//
	controllerName = "sources"

	// consumerName identifies this controller inside the pool, so it only releases its own watches
	consumerName = "sources"

	//
	controllerContextFinishedMessage = "Controller finished by context"

	// cacheSyncTimeout is how long a replica waits for the informer caches before serving
	// admission anyway. A source pointing to a resource type that does not exist in the cluster
	// never syncs, and one broken policy must not take the whole replica out of rotation
	cacheSyncTimeout = 30 * time.Second

	//
	readyWaitingForReconcile = "waiting for the first reconciliation of the policies"
	readyWaitingForCaches    = "waiting for the informer caches"
)

// SourcesControllerOptions represents available options that can be passed to SourcesController on start
type SourcesControllerOptions struct {
	// Duration to wait until resync all the objects
	InformerDurationToResync time.Duration
}

type SourcesControllerDependencies struct {
	Context *context.Context

	// PolicyStores are all the stores whose 'sources' sections must be watched.
	// Add a new policy type by appending its store here
	PolicyStores []policyStore.SourceProvider

	// Pool owns the informers, shared with the rest of the controllers
	Pool *informer.Pool
}

// SourcesController represents a controller that triggers parallel threads.
// These threads watch resources defined in 'sources' section of several object types stored in registries.
// Each thread is an informer in charge of a group of resources GVR (Group + Version + Resource)
type SourcesController struct {
	// Following interface is just needed to register this controller into Controller Runtime manager and let it
	// launch the controller across all the Admitik replicas or just in the elected leader.
	manager.LeaderElectionRunnable

	Options      SourcesControllerOptions
	Dependencies SourcesControllerDependencies

	// carried stuff
	startedAt  time.Time
	reconciled atomic.Bool
	ready      atomic.Bool
}

// Ready reports whether this replica can already serve admission requests.
// It waits for the first reconciliation and for the informer caches, because a replica that has
// not synced would evaluate the policies with empty sources and mutate or validate differently.
// It is a latch: once the replica is ready, adding a source later does not take it out of rotation
func (r *SourcesController) Ready() error {

	if r.ready.Load() {
		return nil
	}

	if !r.reconciled.Load() {
		return errors.New(readyWaitingForReconcile)
	}

	if !r.Dependencies.Pool.Synced() && time.Since(r.startedAt) < cacheSyncTimeout {
		return errors.New(readyWaitingForCaches)
	}

	r.ready.Store(true)
	return nil
}

// NeedLeaderElection implements manager.LeaderElectionRunnable.
// This is needed to inform Controller Runtime manager whether this controller needs a leader or not.
func (r *SourcesController) NeedLeaderElection() bool {
	return false
}

// getSourcesFromPolicies returns a list of sources with all the types registered in suitable registries
func (r *SourcesController) getSourcesFromPolicies() []string {

	var referentCandidates []string

	for _, policyStore := range r.Dependencies.PolicyStores {
		referentCandidates = append(referentCandidates, policyStore.GetReferencedSources()...)
	}

	// Filter duplicated items
	slices.Sort(referentCandidates)
	return slices.Compact(referentCandidates)
}

// Start launches the SourcesController and keeps it alive
// It kills the controller on application's context death, and rerun the process when failed
func (r *SourcesController) Start(ctx context.Context) error {
	logger := log.FromContext(*r.Dependencies.Context).WithValues("controller", controllerName)
	logger.Info("Starting Controller")

	r.startedAt = time.Now()

	ticker := time.NewTicker(secondsToReconcileInformersAgain)
	defer ticker.Stop()

	r.reconcileInformers()
	for {
		select {
		case <-ctx.Done():
			logger.Info(controllerContextFinishedMessage)
			return nil
		case <-ticker.C:
			r.reconcileInformers()
		}
	}
}

// reconcileInformers checks each registered extra-resource type and triggers informers
// for those that are not already started.
func (r *SourcesController) reconcileInformers() {
	r.launchDesiredInformers()
	r.cleanNotNeededInformers()

	r.reconciled.Store(true)
}

// launchDesiredInformers starts an informer for each source type that is not already being watched
func (r *SourcesController) launchDesiredInformers() {
	logger := log.FromContext(*r.Dependencies.Context).WithValues("controller", controllerName)

	for _, resourceType := range r.getSourcesFromPolicies() {

		gvr, err := gvrFromString(resourceType)
		if err != nil {
			logger.Error(err, "Skipping invalid source", "source", resourceType)
			continue
		}

		if _, err := r.Dependencies.Pool.Ensure(consumerName, informer.Scope{GVR: gvr}); err != nil {
			logger.Error(err, "Failed ensuring the informer", "source", resourceType)
		}
	}
}

// cleanNotNeededInformers releases the watches whose source is no longer referenced by any policy
func (r *SourcesController) cleanNotNeededInformers() {
	referentCandidates := r.getSourcesFromPolicies()

	for _, scope := range r.Dependencies.Pool.Owned(consumerName) {
		if !slices.Contains(referentCandidates, common.GvrString(scope.GVR)) {
			r.Dependencies.Pool.Release(consumerName, scope)
		}
	}
}

// gvrFromString parses a source name expressed as {group}/{version}/{resource}
func gvrFromString(resourceType string) (schema.GroupVersionResource, error) {

	resourceTypeParts := strings.Split(resourceType, "/")
	if len(resourceTypeParts) != 3 {
		return schema.GroupVersionResource{}, fmt.Errorf(
			"invalid source '%s': expected {group}/{version}/{resource}", resourceType)
	}

	return schema.GroupVersionResource{
		Group:    resourceTypeParts[0],
		Version:  resourceTypeParts[1],
		Resource: resourceTypeParts[2],
	}, nil
}
