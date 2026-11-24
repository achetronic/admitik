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
	//
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/evaluator"
	"github.com/freepik-company/admitik/internal/globals"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	resourceObserverRegistry "github.com/freepik-company/admitik/internal/registry/resourceobserver"
	"github.com/freepik-company/admitik/internal/template"
)

const (
	ObserverTypeNoop                      = "noop"
	ObserverTypeClusterGenerationPolicies = "clustergenerationpolicies"
)

type EventDispatcherDependencies struct {
	//
	ClusterGenerationPolicyRegistry *policyStore.PolicyStore[*v1alpha1.ClusterGenerationPolicy]
	Evaluator                       *evaluator.Evaluator
	RESTMapper                      meta.RESTMapper
	ResourceObserverRegistry        *resourceObserverRegistry.ResourceObserverRegistry
	Engines                         *template.Registry
}

type EventDispatcher struct {
	dependencies EventDispatcherDependencies

	// Carried stuff
	processors map[string]Processor
}

// NewEventDispatcher TODO
func NewEventDispatcher(deps EventDispatcherDependencies) *EventDispatcher {

	evDispatcher := &EventDispatcher{
		dependencies: deps,
	}

	//
	evDispatcher.processors = evDispatcher.getInitializedProcessors()

	return evDispatcher
}

// getInitializedProcessors return a map with all the processors indexed by type
func (d *EventDispatcher) getInitializedProcessors() (processorsMap map[string]Processor) {
	processors := map[string]Processor{}

	processors[ObserverTypeNoop] = NewNoopProcessor(NoopProcessorDependencies{})

	processors[ObserverTypeClusterGenerationPolicies] = NewGenerationProcessor(GenerationProcessorDependencies{
		ClusterGenerationPolicyRegistry: d.dependencies.ClusterGenerationPolicyRegistry,
		Evaluator:                       d.dependencies.Evaluator,
		Engines:                         d.dependencies.Engines,
		RESTMapper:                      d.dependencies.RESTMapper,
	})

	processorsMap = processors
	return processorsMap
}

// Dispatch TODO
func (d *EventDispatcher) Dispatch(resource string, eventType watch.EventType, object ...map[string]interface{}) {
	logger := log.FromContext(globals.Application.Context)
	_ = logger

	// Skip events when nobody is observing them
	obs := d.dependencies.ResourceObserverRegistry.GetObservers(resource)
	if len(obs) == 0 {
		return
	}

	// Trigger specific observer-related processors
	for _, o := range obs {
		if p, ok := d.processors[o]; ok {
			go p.Process(resource, eventType, object...)
		}
	}
}
