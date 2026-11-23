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

package evaluator

import (
	"errors"
	"strings"

	//
	"k8s.io/apimachinery/pkg/runtime/schema"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/informer"
	"github.com/freepik-company/admitik/internal/registry/policystore"
	"github.com/freepik-company/admitik/internal/telemetry"
	"github.com/freepik-company/admitik/internal/template"
)

// Evaluator prepares the context of a policy over an event and checks its conditions.
// Every policy type uses it, so the steps between the event and the decision live in one place
type Evaluator struct {
	engines *template.Registry
	pool    *informer.Pool
}

func New(engines *template.Registry, pool *informer.Pool) *Evaluator {
	return &Evaluator{engines: engines, pool: pool}
}

// Request is the part of a policy that takes part in the evaluation
type Request struct {
	// Policy holds the sources the policy needs
	Policy policystore.PolicyResourceI

	// Conditions that have to be met
	Conditions []v1alpha1.ConditionT
}

// Result is what the evaluation of the conditions produced
type Result struct {
	// Passed is whether every condition was met
	Passed bool

	// Err is what went wrong while reading the sources or the conditions
	Err error

	// Reason says which stage failed, for the metrics
	Reason telemetry.Reason
}

// Prepare fills the sources of the context and evaluates the conditions of the policy.
// It never stops on a broken source: the ones that could be read are injected and the rest are
// reported in Err, so a failure in one source does not disable the whole policy
func (e *Evaluator) Prepare(request Request, data *template.InjectedData) Result {

	var sourcesErr error

	sources, err := fetchSources(e.engines, e.pool, request.Policy, data)
	if err != nil {
		sourcesErr = err
	}
	data.Sources = sources

	passed, conditionsErr := passingConditions(e.engines, request.Conditions, data)
	if conditionsErr != nil {
		return Result{
			Passed: false,
			Err:    errors.Join(sourcesErr, conditionsErr),
			Reason: telemetry.ReasonConditions,
		}
	}

	reason := telemetry.ReasonNone
	if sourcesErr != nil {
		reason = telemetry.ReasonSources
	}

	return Result{Passed: passed, Err: sourcesErr, Reason: reason}
}

// resourceID returns the resource as {group}/{version}/{resource}
func resourceID(gvr schema.GroupVersionResource) string {
	return strings.Join([]string{gvr.Group, gvr.Version, gvr.Resource}, "/")
}
