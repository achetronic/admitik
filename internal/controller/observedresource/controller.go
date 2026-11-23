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

package observedresource

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	//
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/informer"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	resourceObserverRegistry "github.com/freepik-company/admitik/internal/registry/resourceobserver"
	"github.com/freepik-company/admitik/internal/template"
)

const (
	// secondsToReconcileInformersAgain is the number of seconds to wait
	// between the moment of launching informers, and repeating this process
	// (avoid the spam, mate)
	secondsToReconcileInformersAgain = 2 * time.Second

	//
	controllerName = "observedresource"

	// consumerName identifies this controller inside the pool, so it only releases its own watches
	consumerName = "observedresource"

	//
	controllerContextFinishedMessage = "Controller finished by context"

	resourceInformerParsingError = "Failed to parse the watched resource. Does it look like {group}/{version}/{resource}/{namespace}/{name}?"
)

// ObservedResourceControllerOptions represents available options that can be passed to ObservedResourceController on start
type ObservedResourceControllerOptions struct {
	// Duration to wait until resync all the objects
	InformerDurationToResync time.Duration
}

type ObservedResourceControllerDependencies struct {
	Context *context.Context

	//
	ClusterGenerationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterGenerationPolicy]

	// Pool owns the informers, shared with the rest of the controllers
	Pool *informer.Pool

	ResourceObserverRegistry *resourceObserverRegistry.ResourceObserverRegistry

	// Engines renders the conditions and the generated objects
	Engines *template.Registry
}

// ObservedResourceController represents the controller that triggers parallel threads.
// These threads process coming events against the conditions defined in TODO
// Each thread is an informer in charge of a group of resources GVRNN (Group + Version + Resource + Namespace + Name)
type ObservedResourceController struct {
	// Following interface is just needed to register this controller into Controller Runtime manager and let it
	// launch the controller across all the Admitik replicas or just in the elected leader.
	manager.LeaderElectionRunnable

	//
	Client client.Client

	Options      ObservedResourceControllerOptions
	Dependencies ObservedResourceControllerDependencies

	// Carried stuff
	dispatcher *EventDispatcher
}

// NeedLeaderElection implements manager.LeaderElectionRunnable.
// This is needed to inform Controller Runtime manager whether this controller needs a leader or not.
func (r *ObservedResourceController) NeedLeaderElection() bool {
	return true
}

// getResourcesFromPolicyRegistries returns a list of observers for each type of resource that is required to be watched
// Example: map[resourceType][]Observers
// TODO: Improve docs
func (r *ObservedResourceController) getResourcesFromPolicyRegistries() map[string][]string {

	results := make(map[string][]string)

	candidatesFromGeneration := r.Dependencies.ClusterGenerationPolicyRegistry.Store.GetCollectionNames()
	for _, resourceType := range candidatesFromGeneration {
		if !slices.Contains(results[resourceType], ObserverTypeClusterGenerationPolicies) {
			results[resourceType] = append(results[resourceType], ObserverTypeClusterGenerationPolicies)
		}
	}

	// TODO: Register potential future CRs here

	return results
}

// Start launches the ObservedResourceController and keeps it alive
// It kills the controller on application's context death, and rerun the process when failed
func (r *ObservedResourceController) Start(ctx context.Context) error {
	logger := log.FromContext(*r.Dependencies.Context).WithValues("controller", controllerName)
	logger.Info("Starting Controller")

	// Create an event dispatcher for later usage
	r.dispatcher = NewEventDispatcher(EventDispatcherDependencies{
		ClusterGenerationPolicyRegistry: r.Dependencies.ClusterGenerationPolicyRegistry,
		Pool:                            r.Dependencies.Pool,
		ResourceObserverRegistry:        r.Dependencies.ResourceObserverRegistry,
		Engines:                         r.Dependencies.Engines,
	})

	ticker := time.NewTicker(secondsToReconcileInformersAgain)
	defer ticker.Stop()

	r.reconcileInformers()
	for {
		select {
		case <-ctx.Done():
			logger.Info(controllerContextFinishedMessage)
			return nil
		case <-(*r.Dependencies.Context).Done():
			logger.Info(controllerContextFinishedMessage)
			return nil
		case <-ticker.C:
			r.reconcileInformers()
		}
	}
}

// reconcileInformers makes sure there is a watch for every resource some policy needs to observe,
// and releases the ones nobody needs anymore
func (r *ObservedResourceController) reconcileInformers() {
	logger := log.FromContext(*r.Dependencies.Context).WithValues("controller", controllerName)

	watched := r.getResourcesFromPolicyRegistries()

	for resourceType, observers := range watched {

		scope, err := scopeFromString(resourceType)
		if err != nil {
			logger.Error(err, resourceInformerParsingError, "resource", resourceType)
			continue
		}

		// Store the observers that have to be triggered by this resource
		r.Dependencies.ResourceObserverRegistry.SetObservers(resourceType, observers)

		if _, err := r.Dependencies.Pool.Ensure(consumerName, scope, r.dispatchTo(resourceType)); err != nil {
			logger.Error(err, "Failed ensuring the informer", "resource", resourceType)
		}
	}

	// Release the watches that no policy needs anymore
	for _, scope := range r.Dependencies.Pool.Owned(consumerName) {
		if _, needed := watched[scope.String()]; !needed {
			r.Dependencies.Pool.Release(consumerName, scope)
		}
	}
}

// dispatchTo returns the handler that forwards the events of a watched resource to its observers
func (r *ObservedResourceController) dispatchTo(resourceType string) informer.EventHandlerFunc {

	return func(eventType watch.EventType, obj *unstructured.Unstructured, oldObj *unstructured.Unstructured) error {

		if oldObj == nil {
			r.dispatcher.Dispatch(resourceType, eventType, obj.Object)
			return nil
		}

		r.dispatcher.Dispatch(resourceType, eventType, obj.Object, oldObj.Object)
		return nil
	}
}

// scopeFromString parses a watched resource expressed as {group}/{version}/{resource}/{namespace}/{name}
func scopeFromString(resourceType string) (informer.Scope, error) {

	parts := strings.Split(resourceType, "/")
	if len(parts) != 5 {
		return informer.Scope{}, fmt.Errorf(
			"invalid watched resource '%s': expected {group}/{version}/{resource}/{namespace}/{name}", resourceType)
	}

	return informer.Scope{
		GVR: schema.GroupVersionResource{
			Group:    parts[0],
			Version:  parts[1],
			Resource: parts[2],
		},
		Namespace: parts[3],
		Name:      parts[4],
	}, nil
}
