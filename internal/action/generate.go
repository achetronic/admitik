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

package action

import (
	"context"
	"fmt"
	"strings"
	"time"

	//
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
	"github.com/admitikcore/admitik/internal/common"
	"github.com/admitikcore/admitik/internal/evaluator"
	"github.com/admitikcore/admitik/internal/kubeapply"
	"github.com/admitikcore/admitik/internal/policystore"
	"github.com/admitikcore/admitik/internal/telemetry"
	"github.com/admitikcore/admitik/internal/template"
)

const (
	// GenerationPolicyKind is written on the generated objects as their origin
	GenerationPolicyKind = "ClusterGenerationPolicy"
)

// Generate writes the object a ClusterGenerationPolicy renders for an event
type Generate struct {
	dependencies GenerateDependencies
}

type GenerateDependencies struct {
	Policies  *policystore.PolicyStore[*v1alpha1.ClusterGenerationPolicy]
	Evaluator *evaluator.Evaluator
	Engines   *template.Registry
	Writer    *kubeapply.Writer

	Metrics *telemetry.Recorder
}

func NewGenerate(dependencies GenerateDependencies) *Generate {
	return &Generate{dependencies: dependencies}
}

// Process renders the object of every policy that watches the event and writes it
func (g *Generate) Process(
	ctx context.Context, resourceType string, eventType watch.EventType, object ...map[string]any) {

	logger := log.FromContext(ctx)
	operation := common.GetNormalizedOperation(eventType)
	resource := watchedResourceLabel(resourceType)

	for _, policy := range g.dependencies.Policies.Store.GetResources(resourceType) {

		started := time.Now()

		line := telemetry.DecisionLine{
			Policy:    policy.Name,
			Kind:      telemetry.KindGenerate,
			Operation: operation,
		}

		// The object that woke the policy up, until something is written
		trigger := common.ObjectRefOfObject(object[0])

		outcome, reason, objects, err := g.generate(ctx, policy, eventType, object, &line)
		line.Result = outcome
		line.Reason = reason
		line.Err = err

		extra := []any{"duration", time.Since(started).String()}
		extra = append(extra, triggerField(line, trigger)...)

		telemetry.LogDecision(logger, line, extra...)

		g.dependencies.Metrics.Evaluation(telemetry.KindGenerate, policy.Name, time.Since(started), objects)
		g.dependencies.Metrics.Decision(telemetry.Decision{
			Kind:      telemetry.KindGenerate,
			Policy:    policy.Name,
			Operation: operation,
			Resource:  resource,
			Outcome:   outcome,
			Reason:    reason,
		})
	}
}

// generate renders the object of one policy and writes it. It returns what happened, how many
// source objects were injected and the error, if any
func (g *Generate) generate(
	ctx context.Context,
	policy *v1alpha1.ClusterGenerationPolicy,
	eventType watch.EventType,
	object []map[string]any,
	line *telemetry.DecisionLine,
) (telemetry.Outcome, telemetry.Reason, int, error) {

	// The object that triggered the policy, until something is written
	line.Object = common.ObjectRefOfObject(object[0])

	data := template.NewInjectedData()
	data.Operation = common.GetNormalizedOperation(eventType)
	data.Object = object[0]
	if data.Operation == common.NormalizedOperationUpdate && len(object) > 1 {
		data.OldObject = object[1]
	}

	prepared := g.dependencies.Evaluator.Prepare(evaluator.Request{
		Policy:     policy,
		Conditions: policy.Spec.Conditions,
	}, data)

	sources := countSources(data)

	if !prepared.Passed {
		// A policy whose conditions do not match is not an error, but it is not the same as an
		// object that was already there
		reason := telemetry.ReasonConditions
		if prepared.Err != nil {
			reason = prepared.Reason
		}

		return telemetry.OutcomeSkipped, reason, sources, nil
	}

	rendered, err := g.dependencies.Engines.Evaluate(
		policy.Spec.Object.Definition.Engine, policy.Spec.Object.Definition.Template, data)
	if err != nil {
		return telemetry.OutcomeFailed, telemetry.ReasonTemplate, sources,
			fmt.Errorf("error rendering the object of '%s': %w", policy.Name, err)
	}

	definition := map[string]any{}
	if err = yaml.Unmarshal([]byte(rendered), &definition); err != nil {
		return telemetry.OutcomeFailed, telemetry.ReasonTemplate, sources,
			fmt.Errorf("error decoding the rendered object of '%s': %w", policy.Name, err)
	}

	result, err := g.dependencies.Writer.Apply(ctx, kubeapply.Options{
		PolicyName: policy.Name,
		PolicyKind: GenerationPolicyKind,
		Overwrite:  policy.Spec.OverwriteExisting,
	}, definition)
	if err != nil {
		return telemetry.OutcomeFailed, telemetry.ReasonApply, sources,
			fmt.Errorf("error writing the object of '%s': %w", policy.Name, err)
	}

	// From here the line is about the object that was written, not the one that triggered it
	line.Object = common.ObjectRefOfObject(definition)

	if result.Existing {
		line.Message = "the object exists and 'overwriteExisting' is disabled"
		return telemetry.OutcomeSkipped, telemetry.ReasonNone, sources, nil
	}

	return telemetry.OutcomeGenerated, telemetry.ReasonNone, sources, nil
}

// triggerField names the object that woke the policy up, and only when it is not the object the
// policy acted on. A cleanup policy removes the object that triggered it, so there is nothing to
// tell apart
func triggerField(line telemetry.DecisionLine, trigger common.ObjectRef) []any {

	if line.Object == trigger {
		return nil
	}

	return []any{telemetry.FieldTrigger, trigger}
}

// watchedResourceLabel formats the watched resource, expressed as
// {group}/{version}/{resource}/{namespace}/{name}, as the value of the 'resource' metric label
func watchedResourceLabel(watched string) string {

	parts := strings.Split(watched, "/")
	if len(parts) < 3 {
		return watched
	}

	return common.ResourceLabel(parts[0], parts[1], parts[2])
}

// countSources returns how many objects the policy injected from its sources
func countSources(data *template.InjectedData) int {

	total := 0
	for _, objects := range data.Sources {
		total += len(objects)
	}

	return total
}
