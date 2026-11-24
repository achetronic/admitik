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
	"testing"

	//

	//
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicFake "k8s.io/client-go/dynamic/fake"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
	"github.com/admitikcore/admitik/internal/evaluator"
	"github.com/admitikcore/admitik/internal/informer"
	"github.com/admitikcore/admitik/internal/kubeapply"
	"github.com/admitikcore/admitik/internal/policystore"
	"github.com/admitikcore/admitik/internal/telemetry"
	"github.com/admitikcore/admitik/internal/template"
	"github.com/admitikcore/admitik/internal/template/cel"
	"github.com/admitikcore/admitik/internal/template/gotmpl"
	"github.com/admitikcore/admitik/internal/template/plain"
	"github.com/admitikcore/admitik/internal/template/starlark"
)

// watched is the key the policies are indexed by, as {group}/{version}/{resource}/{namespace}/{name}
const watched = "/v1/namespaces//"

// generatedConfigMap is a valid object for the templates of these tests
const generatedConfigMap = `
apiVersion: v1
kind: ConfigMap
metadata:
  name: generated
  namespace: default
data:
  key: value
`

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

func newTestWriter(t *testing.T) *kubeapply.Writer {
	t.Helper()

	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, meta.RESTScopeNamespace)

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}

	client := dynamicFake.NewSimpleDynamicClientWithCustomListKinds(
		scheme,
		map[schema.GroupVersionResource]string{{Version: "v1", Resource: "configmaps"}: "ConfigMapList"},
	)

	return kubeapply.NewWriter(client, mapper)
}

func generationPolicy(template string, conditions ...v1alpha1.ConditionT) *v1alpha1.ClusterGenerationPolicy {
	return &v1alpha1.ClusterGenerationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "make-configmap"},
		Spec: v1alpha1.ClusterGenerationPolicySpec{
			WatchedResources: []v1alpha1.ResourceGroupT{{
				GroupVersionResource: metav1.GroupVersionResource{Version: "v1", Resource: "namespaces"},
			}},
			Conditions: conditions,
			Object: v1alpha1.ObjectT{
				Definition: v1alpha1.ObjectDefinitionT{Engine: "plain", Template: template},
			},
		},
	}
}

func newTestGenerate(t *testing.T, policy *v1alpha1.ClusterGenerationPolicy) *Generate {
	t.Helper()

	ctx := context.Background()

	store := policystore.NewPolicyStore[*v1alpha1.ClusterGenerationPolicy]()
	store.Store.AddOrUpdateResource(watched, policy)

	return NewGenerate(GenerateDependencies{
		Policies:  store,
		Evaluator: evaluator.New(testEngines(t), informer.NewPool(&ctx, nil, 0)),
		Engines:   testEngines(t),
		Writer:    newTestWriter(t),
	})
}

func watchedObject() map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": "demo"},
	}
}

func TestGenerateWritesTheRenderedObject(t *testing.T) {
	action := newTestGenerate(t, generationPolicy(generatedConfigMap))

	line := &telemetry.DecisionLine{}
	outcome, reason, _, err := action.generate(context.Background(),
		generationPolicy(generatedConfigMap), watch.Added, []map[string]any{watchedObject()}, line)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != telemetry.OutcomeGenerated {
		t.Errorf("got %q as the result, expected %q", outcome, telemetry.OutcomeGenerated)
	}
	if reason != telemetry.ReasonNone {
		t.Errorf("got %q as the reason, expected %q", reason, telemetry.ReasonNone)
	}
	// The line is about the object that was written, not the one that triggered the policy
	if line.Object.Kind != "ConfigMap" || line.Object.Name != "generated" || line.Object.Namespace != "default" {
		t.Errorf("got %+v as the object, expected the generated ConfigMap", line.Object)
	}
}

func TestGenerateSkipsAnObjectThatAlreadyExists(t *testing.T) {
	action := newTestGenerate(t, generationPolicy(generatedConfigMap))
	policy := generationPolicy(generatedConfigMap)

	if _, _, _, err := action.generate(context.Background(), policy,
		watch.Added, []map[string]any{watchedObject()}, &telemetry.DecisionLine{}); err != nil {
		t.Fatalf("unexpected error in the first generation: %v", err)
	}

	// The second pass finds the object already there and 'overwriteExisting' is disabled
	outcome, reason, _, err := action.generate(context.Background(), policy,
		watch.Added, []map[string]any{watchedObject()}, &telemetry.DecisionLine{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != telemetry.OutcomeSkipped {
		t.Errorf("got %q as the outcome, expected %q", outcome, telemetry.OutcomeSkipped)
	}
	if reason != telemetry.ReasonNone {
		t.Errorf("got %q as the reason, expected %q", reason, telemetry.ReasonNone)
	}
}

func TestGenerateSkipsAPolicyWhoseConditionsAreNotMet(t *testing.T) {
	policy := generationPolicy(generatedConfigMap, v1alpha1.ConditionT{
		Name:   "never",
		Engine: "plain",
		Key:    "no",
		Value:  "yes",
	})

	action := newTestGenerate(t, policy)

	line := &telemetry.DecisionLine{}
	outcome, reason, _, err := action.generate(context.Background(), policy,
		watch.Added, []map[string]any{watchedObject()}, line)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome != telemetry.OutcomeSkipped {
		t.Errorf("got %q as the result, expected %q", outcome, telemetry.OutcomeSkipped)
	}
	if reason != telemetry.ReasonConditions {
		t.Errorf("got %q as the reason, expected %q", reason, telemetry.ReasonConditions)
	}
	// Nothing was written, so the line stays about the object that triggered the policy
	if line.Object.Kind != "Namespace" || line.Object.Name != "demo" {
		t.Errorf("got %+v as the object, expected the watched Namespace", line.Object)
	}
}

func TestGenerateFailsWhenTheRenderedObjectIsNotYAML(t *testing.T) {
	policy := generationPolicy("this is not an object")

	action := newTestGenerate(t, policy)

	outcome, reason, _, err := action.generate(context.Background(), policy,
		watch.Added, []map[string]any{watchedObject()}, &telemetry.DecisionLine{})

	if err == nil {
		t.Fatal("expected an error")
	}
	if outcome != telemetry.OutcomeFailed {
		t.Errorf("got %q as the outcome, expected %q", outcome, telemetry.OutcomeFailed)
	}
	if reason != telemetry.ReasonTemplate {
		t.Errorf("got %q as the reason, expected %q", reason, telemetry.ReasonTemplate)
	}
}

func TestWatchedResourceLabel(t *testing.T) {
	cases := map[string]string{
		"/v1/namespaces//":                          "v1/namespaces",
		"apps/v1/deployments/default/api":           "apps/v1/deployments",
		"gateway.networking.k8s.io/v1/httproutes//": "gateway.networking.k8s.io/v1/httproutes",
	}

	for watched, expected := range cases {
		if got := watchedResourceLabel(watched); got != expected {
			t.Errorf("got %q for %q, expected %q", got, watched, expected)
		}
	}
}
