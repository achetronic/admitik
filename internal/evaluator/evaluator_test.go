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

package evaluator_test

import (
	"context"
	"testing"

	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicFake "k8s.io/client-go/dynamic/fake"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/evaluator"
	"github.com/freepik-company/admitik/internal/informer"
	"github.com/freepik-company/admitik/internal/telemetry"
	"github.com/freepik-company/admitik/internal/template"
	"github.com/freepik-company/admitik/internal/template/cel"
	"github.com/freepik-company/admitik/internal/template/gotmpl"
	"github.com/freepik-company/admitik/internal/template/plain"
	"github.com/freepik-company/admitik/internal/template/starlark"
)

type testPolicy struct {
	name    string
	sources []v1alpha1.SourceGroupT
}

func (p *testPolicy) GetUniqueIdentifier() string         { return p.name }
func (p *testPolicy) GetSources() []v1alpha1.SourceGroupT { return p.sources }

func testEngines(t *testing.T) *template.Registry {
	t.Helper()

	celExpression, err := cel.NewExpression()
	if err != nil {
		t.Fatalf("error creating the CEL engine: %v", err)
	}

	return template.NewRegistry(
		celExpression,
		cel.NewPlaceholders(celExpression),
		gotmpl.New(),
		starlark.New(),
		plain.New(),
	)
}

func newEvaluator(t *testing.T) *evaluator.Evaluator {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pool := informer.NewPool(&ctx, dynamicFake.NewSimpleDynamicClient(runtime.NewScheme()), 0)

	return evaluator.New(testEngines(t), pool)
}

func testData() *template.InjectedData {
	data := template.NewInjectedData()
	data.Operation = "CREATE"
	data.Object = map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "demo", "namespace": "default"},
	}

	return data
}

func TestPreparePassesWhenTheConditionsAreMet(t *testing.T) {
	result := newEvaluator(t).Prepare(evaluator.Request{
		Policy: &testPolicy{name: "policy"},
		Conditions: []v1alpha1.ConditionT{{
			Name:   "always-true",
			Engine: "cel",
			Key:    "object.metadata.name",
			Value:  "demo",
		}},
	}, testData())

	if !result.Passed {
		t.Error("the conditions should have been met")
	}
	if result.Err != nil {
		t.Errorf("unexpected error: %v", result.Err)
	}
	if result.Reason != telemetry.ReasonNone {
		t.Errorf("got reason %q, expected none", result.Reason)
	}
}

func TestPrepareFailsWhenAConditionIsNotMet(t *testing.T) {
	result := newEvaluator(t).Prepare(evaluator.Request{
		Policy: &testPolicy{name: "policy"},
		Conditions: []v1alpha1.ConditionT{{
			Name:   "never-true",
			Engine: "cel",
			Key:    "object.metadata.name",
			Value:  "another",
		}},
	}, testData())

	if result.Passed {
		t.Error("the conditions should not have been met")
	}
	if result.Err != nil {
		t.Errorf("a condition that is not met is not an error, got %v", result.Err)
	}
}

func TestPrepareReportsABrokenCondition(t *testing.T) {
	result := newEvaluator(t).Prepare(evaluator.Request{
		Policy: &testPolicy{name: "policy"},
		Conditions: []v1alpha1.ConditionT{{
			Name:   "does-not-compile",
			Engine: "cel",
			Key:    "object.this.does.not.exist",
			Value:  "true",
		}},
	}, testData())

	if result.Passed {
		t.Error("a policy that cannot be evaluated must not pass")
	}
	if result.Err == nil {
		t.Error("expected an error")
	}
	if result.Reason != telemetry.ReasonConditions {
		t.Errorf("got reason %q, expected conditions", result.Reason)
	}
}

func TestPrepareReportsAMissingSource(t *testing.T) {
	// The resource type of the source is not watched, so the policy gets no objects
	result := newEvaluator(t).Prepare(evaluator.Request{
		Policy: &testPolicy{
			name: "policy",
			sources: []v1alpha1.SourceGroupT{{
				GroupVersionResource: metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			}},
		},
		Conditions: []v1alpha1.ConditionT{{
			Name:   "no-objects",
			Engine: "cel",
			Key:    "sources[0].size()",
			Value:  "0",
		}},
	}, testData())

	if !result.Passed {
		t.Errorf("a policy with empty sources can still pass, got %v", result.Err)
	}
	if result.Err != nil {
		t.Errorf("an unwatched source is not an error, got %v", result.Err)
	}
}

func TestPrepareFillsTheSourcesOfTheContext(t *testing.T) {
	evaluatorWithSources := newEvaluator(t)
	data := testData()

	result := evaluatorWithSources.Prepare(evaluator.Request{
		Policy: &testPolicy{name: "policy", sources: []v1alpha1.SourceGroupT{{
			GroupVersionResource: metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
		}}},
	}, data)

	if data.Sources == nil {
		t.Fatal("the context was not filled with the sources")
	}
	if len(data.Sources) != 1 {
		t.Errorf("got %d sources, expected one entry per source of the policy", len(data.Sources))
	}
	if _, found := data.Sources[0]; !found {
		t.Error("the sources are not indexed by their position in the policy")
	}
	if !result.Passed {
		t.Error("a policy without conditions always passes")
	}
}

func TestPrepareWithoutConditionsAlwaysPasses(t *testing.T) {
	result := newEvaluator(t).Prepare(evaluator.Request{
		Policy: &testPolicy{name: "policy"},
	}, testData())

	if !result.Passed {
		t.Error("a policy without conditions must pass")
	}
}

func TestPrepareWithABrokenSourceKeepsEvaluating(t *testing.T) {
	// A source with an invalid CEL expression in its filters is reported, but the conditions
	// are still evaluated
	policy := &testPolicy{
		name: "policy",
		sources: []v1alpha1.SourceGroupT{{
			GroupVersionResource: metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			Filters: &v1alpha1.SourceGroupFiltersT{
				Namespace: &v1alpha1.SourceGroupFiltersNamespaceT{
					MatchList: []string{"{{cel: object.this.does.not.exist}}"},
				},
			},
		}},
	}

	result := newEvaluator(t).Prepare(evaluator.Request{
		Policy: policy,
		Conditions: []v1alpha1.ConditionT{{
			Name:   "always-true",
			Engine: "plain",
			Key:    "yes",
			Value:  "yes",
		}},
	}, testData())

	if !result.Passed {
		t.Error("a broken source must not stop the conditions")
	}
	if result.Err == nil {
		t.Error("the broken source should be reported")
	}
	if result.Reason != telemetry.ReasonSources {
		t.Errorf("got reason %q, expected sources", result.Reason)
	}
}
