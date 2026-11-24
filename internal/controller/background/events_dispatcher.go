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

	//
	"k8s.io/apimachinery/pkg/watch"
)

// ObserverTypeClusterGenerationPolicies is the name the ClusterGenerationPolicy controllers use
// when they ask for the events of a resource
const ObserverTypeClusterGenerationPolicies = "clustergenerationpolicies"

// EventDispatcher hands the events of a watched resource to the processors that asked for them
type EventDispatcher struct {
	context    context.Context
	observers  *observers
	processors map[string]Processor
}

type EventDispatcherDependencies struct {
	// Context is the one of the application, used to run the processors
	Context context.Context

	// Observers says which processors want the events of each resource
	Observers *observers

	// Processors are indexed by the name the policies use to observe a resource
	Processors map[string]Processor
}

func NewEventDispatcher(dependencies EventDispatcherDependencies) *EventDispatcher {

	return &EventDispatcher{
		context:    dependencies.Context,
		observers:  dependencies.Observers,
		processors: dependencies.Processors,
	}
}

// Dispatch runs the processors that observe the resource, one goroutine each
func (d *EventDispatcher) Dispatch(resource string, eventType watch.EventType, object ...map[string]any) {

	interested := d.observers.get(resource)
	if len(interested) == 0 {
		return
	}

	for _, name := range interested {
		processor, found := d.processors[name]
		if !found {
			continue
		}

		go processor.Process(d.context, resource, eventType, object...)
	}
}
