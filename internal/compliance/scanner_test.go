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

package compliance

import (
	"context"
	"slices"
	"testing"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
	"github.com/admitikcore/admitik/internal/evaluator"
	"github.com/admitikcore/admitik/internal/informer"
	"github.com/admitikcore/admitik/internal/template"
	"github.com/admitikcore/admitik/internal/template/cel"
	"github.com/admitikcore/admitik/internal/template/gotmpl"
	"github.com/admitikcore/admitik/internal/template/plain"
	"github.com/admitikcore/admitik/internal/template/starlark"
)

var configMapsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "configmaps"}

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

func configMap(name string, labels map[string]any) *unstructured.Unstructured {

	metadata := map[string]any{"name": name, "namespace": "default"}
	if labels != nil {
		metadata["labels"] = labels
	}

	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   metadata,
		"data":       map[string]any{"key": "value"},
	}}
}

func validationPolicy(name string, condition v1alpha1.ConditionT) *v1alpha1.ClusterValidationPolicy {
	return &v1alpha1.ClusterValidationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ClusterValidationPolicySpec{
			FailureAction: v1alpha1.ValidationFailureActionEnforce,
			InterceptedResources: []v1alpha1.AdmissionResourceGroupT{{
				GroupVersionResource: metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
				Operations:           []admissionregv1.OperationType{admissionregv1.Create},
			}},
			Conditions: []v1alpha1.ConditionT{condition},
			Message:    v1alpha1.MessageT{Engine: "plain", Template: "not allowed"},
		},
	}
}

func newTestScanner(t *testing.T, options ScannerOptions, policies []*v1alpha1.ClusterValidationPolicy,
	objects ...runtime.Object) *Scanner {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme for v1alpha1: %v", err)
	}

	objectsToStore := make([]client.Object, 0, len(policies))
	for _, policy := range policies {
		objectsToStore = append(objectsToStore, policy)
	}

	reader := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objectsToStore...).
		Build()

	dynamicClient := dynamicFake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{configMapsGVR: "ConfigMapList"},
		objects...,
	)

	ctx := context.Background()

	return NewScanner(options, ScannerDependencies{
		Client:    reader,
		Dynamic:   dynamicClient,
		Evaluator: evaluator.New(testEngines(t), informer.NewPool(&ctx, nil, 0)),
	})
}

func TestScanCountsCompliantAndNonCompliantObjects(t *testing.T) {
	policy := validationPolicy("needs-team-label", v1alpha1.ConditionT{
		Name:   "has-team-label",
		Engine: "cel",
		Key:    "has(object.metadata.labels) && 'team' in object.metadata.labels",
		Value:  "true",
	})

	scanner := newTestScanner(t, ScannerOptions{}, []*v1alpha1.ClusterValidationPolicy{policy},
		configMap("with-label", map[string]any{"team": "platform"}),
		configMap("without-label", nil),
		configMap("other-labels", map[string]any{"app": "demo"}),
	)

	scan, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scan.Summary.Scanned != 3 {
		t.Errorf("got %d scanned, expected 3", scan.Summary.Scanned)
	}
	if scan.Summary.Compliant != 1 {
		t.Errorf("got %d compliant, expected 1", scan.Summary.Compliant)
	}
	if scan.Summary.NonCompliant != 2 {
		t.Errorf("got %d non compliant, expected 2", scan.Summary.NonCompliant)
	}
	if len(scan.Results) != 1 {
		t.Fatalf("got %d results, expected 1", len(scan.Results))
	}
	if scan.Results[0].Resource != "v1/configmaps" {
		t.Errorf("got %q as the resource, expected v1/configmaps", scan.Results[0].Resource)
	}
}

func TestScanKeepsAFewSamples(t *testing.T) {
	policy := validationPolicy("never-passes", v1alpha1.ConditionT{
		Name:   "always-fails",
		Engine: "plain",
		Key:    "no",
		Value:  "yes",
	})

	scanner := newTestScanner(t, ScannerOptions{}, []*v1alpha1.ClusterValidationPolicy{policy},
		configMap("one", nil),
		configMap("two", nil),
		configMap("three", nil),
		configMap("four", nil),
		configMap("five", nil),
		configMap("six", nil),
	)

	scan, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	samples := scan.Results[0].Samples
	if len(samples) != defaultMaxSamples {
		t.Fatalf("got %d samples, expected %d", len(samples), defaultMaxSamples)
	}
	if !slices.Contains(samples, "default/one") {
		t.Errorf("got %v, expected the objects as namespace/name", samples)
	}
}

func TestScanGoesThroughEveryPage(t *testing.T) {
	policy := validationPolicy("never-passes", v1alpha1.ConditionT{
		Name:   "always-fails",
		Engine: "plain",
		Key:    "no",
		Value:  "yes",
	})

	scanner := newTestScanner(t, ScannerOptions{PageSize: 2}, []*v1alpha1.ClusterValidationPolicy{policy},
		configMap("one", nil),
		configMap("two", nil),
		configMap("three", nil),
		configMap("four", nil),
		configMap("five", nil),
	)

	scan, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scan.Summary.Scanned != 5 {
		t.Errorf("got %d scanned, expected 5", scan.Summary.Scanned)
	}
	if scan.Summary.NonCompliant != 5 {
		t.Errorf("got %d non compliant, expected 5", scan.Summary.NonCompliant)
	}
}

func TestScanOfAClusterWithoutPolicies(t *testing.T) {
	scanner := newTestScanner(t, ScannerOptions{}, nil)

	scan, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scan.Summary.Policies != 0 || len(scan.Results) != 0 {
		t.Errorf("got %+v, expected an empty scan", scan.Summary)
	}
}

func TestScanWithoutObjects(t *testing.T) {
	policy := validationPolicy("never-passes", v1alpha1.ConditionT{
		Name:   "always-fails",
		Engine: "plain",
		Key:    "no",
		Value:  "yes",
	})

	scanner := newTestScanner(t, ScannerOptions{}, []*v1alpha1.ClusterValidationPolicy{policy})

	scan, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scan.Summary.Scanned != 0 {
		t.Errorf("got %d scanned, expected 0", scan.Summary.Scanned)
	}
	if scan.Summary.Policies != 1 {
		t.Errorf("got %d policies, expected 1", scan.Summary.Policies)
	}
}

func TestScanCountsErrors(t *testing.T) {
	// A policy that cannot be evaluated is reported as an error, not as non compliant
	policy := validationPolicy("broken", v1alpha1.ConditionT{
		Name:   "does-not-compile",
		Engine: "cel",
		Key:    "object.this.does.not.exist",
		Value:  "true",
	})

	scanner := newTestScanner(t, ScannerOptions{}, []*v1alpha1.ClusterValidationPolicy{policy},
		configMap("one", nil),
	)

	scan, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if scan.Summary.Errors != 1 {
		t.Errorf("got %d errors, expected 1", scan.Summary.Errors)
	}
	if scan.Summary.NonCompliant != 0 {
		t.Errorf("got %d non compliant, expected 0", scan.Summary.NonCompliant)
	}
}

func TestScanOfAPolicyThatInterceptsTheSameResourceTwice(t *testing.T) {
	policy := validationPolicy("never-passes", v1alpha1.ConditionT{
		Name:   "always-fails",
		Engine: "plain",
		Key:    "no",
		Value:  "yes",
	})
	policy.Spec.InterceptedResources = append(policy.Spec.InterceptedResources,
		policy.Spec.InterceptedResources[0])

	scanner := newTestScanner(t, ScannerOptions{}, []*v1alpha1.ClusterValidationPolicy{policy},
		configMap("one", nil),
	)

	scan, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(scan.Results) != 1 {
		t.Fatalf("got %d results, expected the resource once", len(scan.Results))
	}
	if scan.Summary.Scanned != 1 {
		t.Errorf("got %d scanned, expected 1", scan.Summary.Scanned)
	}
}

func TestToTelemetryKeepsWhatTheMetricsNeed(t *testing.T) {
	converted := ToTelemetry([]Result{{
		Policy:       "require-limits",
		Resource:     "v1/pods",
		Compliant:    8,
		NonCompliant: 2,
		Errors:       1,
		Samples:      []string{"default/one"},
	}})

	if len(converted) != 1 {
		t.Fatalf("got %d results, expected 1", len(converted))
	}
	if converted[0].Policy != "require-limits" || converted[0].Resource != "v1/pods" {
		t.Errorf("got %+v, expected the policy and the resource", converted[0])
	}
	if converted[0].Compliant != 8 || converted[0].NonCompliant != 2 || converted[0].Errors != 1 {
		t.Errorf("got %+v, expected the three counts", converted[0])
	}
}
