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

package audit

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
	"k8s.io/apimachinery/pkg/types"
	dynamicFake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/evaluator"
	"github.com/freepik-company/admitik/internal/informer"
	"github.com/freepik-company/admitik/internal/template"
	"github.com/freepik-company/admitik/internal/template/cel"
	"github.com/freepik-company/admitik/internal/template/gotmpl"
	"github.com/freepik-company/admitik/internal/template/plain"
	"github.com/freepik-company/admitik/internal/template/starlark"
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

func newTestScanner(t *testing.T, policies []*v1alpha1.ClusterValidationPolicy, objects ...runtime.Object) (*Scanner, client.Client) {
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
		WithStatusSubresource(&v1alpha1.ClusterAuditReport{}).
		Build()

	dynamicClient := dynamicFake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{configMapsGVR: "ConfigMapList"},
		objects...,
	)

	ctx := context.Background()

	return NewScanner(ScannerOptions{}, ScannerDependencies{
		Client:    reader,
		Dynamic:   dynamicClient,
		Evaluator: evaluator.New(testEngines(t), informer.NewPool(&ctx, nil, 0)),
	}), reader
}

func TestScanCountsCompliantAndNonCompliantObjects(t *testing.T) {
	policy := validationPolicy("needs-team-label", v1alpha1.ConditionT{
		Name:   "has-team-label",
		Engine: "cel",
		Key:    "has(object.metadata.labels) && 'team' in object.metadata.labels",
		Value:  "true",
	})

	scanner, _ := newTestScanner(t, []*v1alpha1.ClusterValidationPolicy{policy},
		configMap("with-label", map[string]any{"team": "platform"}),
		configMap("without-label", nil),
		configMap("other-labels", map[string]any{"app": "demo"}),
	)

	report, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status.Summary.Scanned != 3 {
		t.Errorf("got %d scanned, expected 3", report.Status.Summary.Scanned)
	}
	if report.Status.Summary.Compliant != 1 {
		t.Errorf("got %d compliant, expected 1", report.Status.Summary.Compliant)
	}
	if report.Status.Summary.NonCompliant != 2 {
		t.Errorf("got %d non compliant, expected 2", report.Status.Summary.NonCompliant)
	}
	if len(report.Status.Results) != 1 {
		t.Fatalf("got %d results, expected 1", len(report.Status.Results))
	}
	if report.Status.Results[0].Resource != "/v1/configmaps" {
		t.Errorf("got %q as the resource, expected /v1/configmaps", report.Status.Results[0].Resource)
	}
}

func TestScanKeepsAFewSamples(t *testing.T) {
	policy := validationPolicy("never-passes", v1alpha1.ConditionT{
		Name:   "always-fails",
		Engine: "plain",
		Key:    "no",
		Value:  "yes",
	})

	scanner, _ := newTestScanner(t, []*v1alpha1.ClusterValidationPolicy{policy},
		configMap("one", nil),
		configMap("two", nil),
		configMap("three", nil),
		configMap("four", nil),
		configMap("five", nil),
		configMap("six", nil),
	)

	report, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	samples := report.Status.Results[0].Samples
	if len(samples) != DefaultMaxSamples {
		t.Fatalf("got %d samples, expected %d", len(samples), DefaultMaxSamples)
	}
	if !slices.Contains(samples, "default/one") {
		t.Errorf("got %v, expected the objects as namespace/name", samples)
	}
}

func TestScanWritesOneReport(t *testing.T) {
	policy := validationPolicy("never-passes", v1alpha1.ConditionT{
		Name:   "always-fails",
		Engine: "plain",
		Key:    "no",
		Value:  "yes",
	})

	scanner, reader := newTestScanner(t, []*v1alpha1.ClusterValidationPolicy{policy},
		configMap("one", nil),
	)

	ctx := context.Background()
	if _, err := scanner.Scan(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A second scan must update the same object, not create another one
	if _, err := scanner.Scan(ctx); err != nil {
		t.Fatalf("unexpected error in the second scan: %v", err)
	}

	reports := &v1alpha1.ClusterAuditReportList{}
	if err := reader.List(ctx, reports); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reports.Items) != 1 {
		t.Fatalf("got %d reports, expected 1", len(reports.Items))
	}
	if reports.Items[0].Status.Summary.NonCompliant != 1 {
		t.Errorf("got %d non compliant, expected 1",
			reports.Items[0].Status.Summary.NonCompliant)
	}
}

func TestScanOfAClusterWithoutPolicies(t *testing.T) {
	scanner, _ := newTestScanner(t, nil)

	report, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status.Summary.Policies != 0 || len(report.Status.Results) != 0 {
		t.Errorf("got %+v, expected an empty report", report.Status)
	}
}

func TestScanWithoutObjects(t *testing.T) {
	policy := validationPolicy("never-passes", v1alpha1.ConditionT{
		Name:   "always-fails",
		Engine: "plain",
		Key:    "no",
		Value:  "yes",
	})

	scanner, _ := newTestScanner(t, []*v1alpha1.ClusterValidationPolicy{policy})

	report, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status.Summary.Scanned != 0 {
		t.Errorf("got %d scanned, expected 0", report.Status.Summary.Scanned)
	}
	if report.Status.Summary.Policies != 1 {
		t.Errorf("got %d policies, expected 1", report.Status.Summary.Policies)
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

	scanner, _ := newTestScanner(t, []*v1alpha1.ClusterValidationPolicy{policy},
		configMap("one", nil),
	)

	report, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status.Summary.Errors != 1 {
		t.Errorf("got %d errors, expected 1", report.Status.Summary.Errors)
	}
	if report.Status.Summary.NonCompliant != 0 {
		t.Errorf("got %d non compliant, expected 0", report.Status.Summary.NonCompliant)
	}
}

func TestReportName(t *testing.T) {
	policy := validationPolicy("never-passes", v1alpha1.ConditionT{
		Name:   "always-fails",
		Engine: "plain",
		Key:    "no",
		Value:  "yes",
	})

	scanner, reader := newTestScanner(t, []*v1alpha1.ClusterValidationPolicy{policy})

	if _, err := scanner.Scan(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report := &v1alpha1.ClusterAuditReport{}
	if err := reader.Get(context.Background(), types.NamespacedName{Name: ReportName}, report); err != nil {
		t.Fatalf("the report was not created with the expected name: %v", err)
	}
}
