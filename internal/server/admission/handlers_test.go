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

package admission

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	//
	admissionv1 "k8s.io/api/admission/v1"
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/informer"
	policyStore "github.com/freepik-company/admitik/internal/registry/policystore"
	"github.com/freepik-company/admitik/internal/template"
	"github.com/freepik-company/admitik/internal/template/cel"
	"github.com/freepik-company/admitik/internal/template/gotmpl"
	"github.com/freepik-company/admitik/internal/template/plain"
	"github.com/freepik-company/admitik/internal/template/starlark"
)

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

func testServer(t *testing.T, mutation []*v1alpha1.ClusterMutationPolicy, validation []*v1alpha1.ClusterValidationPolicy) *HttpServer {
	t.Helper()

	ctx := context.Background()

	mutationStore := policyStore.NewPolicyStore[*v1alpha1.ClusterMutationPolicy]()
	for _, policy := range mutation {
		for _, watchedType := range []string{"/v1/configmaps/CREATE"} {
			mutationStore.Store.AddOrUpdateResource(watchedType, policy)
		}
	}

	validationStore := policyStore.NewPolicyStore[*v1alpha1.ClusterValidationPolicy]()
	for _, policy := range validation {
		validationStore.Store.AddOrUpdateResource("/v1/configmaps/CREATE", policy)
	}

	return &HttpServer{
		dependencies: &AdmissionServerDependencies{
			Context:                         &ctx,
			ClusterMutationPolicyRegistry:   mutationStore,
			ClusterValidationPolicyRegistry: validationStore,
			// The policies used by these tests declare no sources, so the pool is never asked
			Pool:    informer.NewPool(&ctx, nil, 0),
			Engines: testEngines(t),
		},
	}
}

func reviewRequest(t *testing.T, operation admissionv1.Operation, object map[string]any) *http.Request {
	t.Helper()

	objectBytes, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	review := admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			UID:       "test",
			Operation: operation,
			Object:    runtime.RawExtension{Raw: objectBytes},
			Resource:  metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			Name:      "demo",
			Namespace: "default",
		},
	}

	body, err := json.Marshal(review)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/admission/mutation", bytes.NewReader(body))

	return request
}

func decodeResponse(t *testing.T, recorder *httptest.ResponseRecorder) *admissionv1.AdmissionResponse {
	t.Helper()

	review := admissionv1.AdmissionReview{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &review); err != nil {
		t.Fatalf("the response is not an AdmissionReview: %v", err)
	}
	if review.Response == nil {
		t.Fatal("the response is empty")
	}

	return review.Response
}

func testObject() map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "demo", "namespace": "default"},
		"data":       map[string]any{"key": "value"},
	}
}

func mutationPolicy(name, patchType, engine, patchTemplate string) *v1alpha1.ClusterMutationPolicy {
	return &v1alpha1.ClusterMutationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ClusterMutationPolicySpec{
			InterceptedResources: []v1alpha1.AdmissionResourceGroupT{{
				GroupVersionResource: metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
				Operations:           []admissionregv1.OperationType{admissionregv1.Create},
			}},
			Patch: v1alpha1.PatchT{Type: patchType, Engine: engine, Template: patchTemplate},
		},
	}
}

func TestMutationAppliesThePatch(t *testing.T) {
	server := testServer(t, []*v1alpha1.ClusterMutationPolicy{
		mutationPolicy("ok", "jsonmerge", "plain+cel", "metadata:\n  labels:\n    patched: \"yes\"\n"),
	}, nil)

	recorder := httptest.NewRecorder()
	server.handleMutationRequest(recorder, reviewRequest(t, admissionv1.Create, testObject()))

	response := decodeResponse(t, recorder)
	if !response.Allowed {
		t.Error("the object should be allowed")
	}
	if len(response.Warnings) != 0 {
		t.Errorf("got warnings %v, expected none", response.Warnings)
	}
	if !strings.Contains(string(response.Patch), "patched") {
		t.Errorf("the patch does not contain the label: %s", response.Patch)
	}
}

func TestMutationWarnsWhenTheTemplateFails(t *testing.T) {
	server := testServer(t, []*v1alpha1.ClusterMutationPolicy{
		mutationPolicy("broken", "jsonmerge", "cel", "object.this.does.not.exist"),
	}, nil)

	recorder := httptest.NewRecorder()
	server.handleMutationRequest(recorder, reviewRequest(t, admissionv1.Create, testObject()))

	response := decodeResponse(t, recorder)
	if !response.Allowed {
		t.Error("a mutation failure must not reject the object")
	}
	if len(response.Warnings) != 1 {
		t.Fatalf("got warnings %v, expected one", response.Warnings)
	}
	if !strings.Contains(response.Warnings[0], "broken") {
		t.Errorf("the warning does not name the policy: %s", response.Warnings[0])
	}
}

func TestMutationWarnsWhenThePatchIsInvalid(t *testing.T) {
	server := testServer(t, []*v1alpha1.ClusterMutationPolicy{
		mutationPolicy("invalid-patch", "jsonpatch", "plain", "{not a patch}"),
	}, nil)

	recorder := httptest.NewRecorder()
	server.handleMutationRequest(recorder, reviewRequest(t, admissionv1.Create, testObject()))

	response := decodeResponse(t, recorder)
	if len(response.Warnings) != 1 {
		t.Fatalf("got warnings %v, expected one", response.Warnings)
	}
	if !strings.Contains(response.Warnings[0], "invalid patch") {
		t.Errorf("unexpected warning: %s", response.Warnings[0])
	}
}

func TestMutationWithoutPoliciesDoesNothing(t *testing.T) {
	server := testServer(t, nil, nil)

	recorder := httptest.NewRecorder()
	server.handleMutationRequest(recorder, reviewRequest(t, admissionv1.Create, testObject()))

	response := decodeResponse(t, recorder)
	if !response.Allowed {
		t.Error("the object should be allowed")
	}
	if len(response.Warnings) != 0 {
		t.Errorf("got warnings %v, expected none", response.Warnings)
	}
}

func validationPolicy(name, failureAction, message string) *v1alpha1.ClusterValidationPolicy {
	return &v1alpha1.ClusterValidationPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.ClusterValidationPolicySpec{
			FailureAction: failureAction,
			InterceptedResources: []v1alpha1.AdmissionResourceGroupT{{
				GroupVersionResource: metav1.GroupVersionResource{Version: "v1", Resource: "configmaps"},
				Operations:           []admissionregv1.OperationType{admissionregv1.Create},
			}},
			Conditions: []v1alpha1.ConditionT{{
				Name:   "always-fails",
				Engine: "plain",
				Key:    "no",
				Value:  "yes",
			}},
			Message: v1alpha1.MessageT{Engine: "plain", Template: message},
		},
	}
}

func TestValidationRejectsWithThePolicyMessage(t *testing.T) {
	server := testServer(t, nil, []*v1alpha1.ClusterValidationPolicy{
		validationPolicy("enforced", v1alpha1.ValidationFailureActionEnforce, "this object is not allowed"),
	})

	recorder := httptest.NewRecorder()
	server.handleValidationRequest(recorder, reviewRequest(t, admissionv1.Create, testObject()))

	response := decodeResponse(t, recorder)
	if response.Allowed {
		t.Error("the object should be rejected")
	}
	if response.Result == nil || response.Result.Message != "this object is not allowed" {
		t.Errorf("got %v as the message", response.Result)
	}
}

func TestValidationInPermissiveModeWarnsAndAllows(t *testing.T) {
	server := testServer(t, nil, []*v1alpha1.ClusterValidationPolicy{
		validationPolicy("permissive", v1alpha1.ValidationFailureActionPermissive, "violation found"),
	})

	recorder := httptest.NewRecorder()
	server.handleValidationRequest(recorder, reviewRequest(t, admissionv1.Create, testObject()))

	response := decodeResponse(t, recorder)
	if !response.Allowed {
		t.Error("a permissive policy must not reject the object")
	}
	if len(response.Warnings) != 1 {
		t.Fatalf("got warnings %v, expected one", response.Warnings)
	}
	if !strings.Contains(response.Warnings[0], "permissive") {
		t.Errorf("unexpected warning: %s", response.Warnings[0])
	}
}

func TestValidationWarnsWhenTheMessageFails(t *testing.T) {
	policy := validationPolicy("broken-message", v1alpha1.ValidationFailureActionEnforce, "")
	policy.Spec.Message = v1alpha1.MessageT{Engine: "cel", Template: "object.this.does.not.exist"}

	server := testServer(t, nil, []*v1alpha1.ClusterValidationPolicy{policy})

	recorder := httptest.NewRecorder()
	server.handleValidationRequest(recorder, reviewRequest(t, admissionv1.Create, testObject()))

	response := decodeResponse(t, recorder)
	if response.Allowed {
		t.Error("the object should be rejected when the policy cannot be evaluated")
	}
	if len(response.Warnings) != 1 {
		t.Fatalf("got warnings %v, expected one", response.Warnings)
	}
}

func TestValidationAllowsWhenTheConditionsPass(t *testing.T) {
	policy := validationPolicy("passing", v1alpha1.ValidationFailureActionEnforce, "never shown")
	policy.Spec.Conditions = []v1alpha1.ConditionT{{
		Name:   "always-passes",
		Engine: "plain",
		Key:    "yes",
		Value:  "yes",
	}}

	server := testServer(t, nil, []*v1alpha1.ClusterValidationPolicy{policy})

	recorder := httptest.NewRecorder()
	server.handleValidationRequest(recorder, reviewRequest(t, admissionv1.Create, testObject()))

	response := decodeResponse(t, recorder)
	if !response.Allowed {
		t.Error("the object should be allowed")
	}
	if len(response.Warnings) != 0 {
		t.Errorf("got warnings %v, expected none", response.Warnings)
	}
}
