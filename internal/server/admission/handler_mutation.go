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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	//
	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/wI2L/jsondiff"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
	"github.com/admitikcore/admitik/internal/common"
	"github.com/admitikcore/admitik/internal/evaluator"
	"github.com/admitikcore/admitik/internal/globals"
	"github.com/admitikcore/admitik/internal/telemetry"
	"github.com/admitikcore/admitik/internal/template"
)

// handleRequest handles the incoming requests
func (s *HttpServer) handleMutationRequest(response http.ResponseWriter, request *http.Request) {
	logger := log.FromContext(request.Context()).WithValues("controller", "admissionserver")

	var err error

	// Read the request
	bodyBytes, err := io.ReadAll(request.Body)
	if err != nil {
		logger.Info(fmt.Sprintf("failed reading request.body: %s", err.Error()))
		return
	}

	//
	requestObj := admissionv1.AdmissionReview{}
	err = json.Unmarshal(bodyBytes, &requestObj)
	if err != nil {
		logger.Info(fmt.Sprintf("failed decoding JSON from request.body: %s", err.Error()))
		return
	}

	// The object every line of this handler is about
	objectRef := common.ObjectRefOfAdmission(requestObj.Request.Kind, requestObj.Request.Namespace, requestObj.Request.Name)

	logger.V(1).Info("object under mutation review", "object", objectRef)

	// Assume that the request is allowed by default
	// Done in purpose as validation webhooks are executed later
	reviewResponse := &requestObj
	reviewResponse.Response = &admissionv1.AdmissionResponse{
		UID:     requestObj.Request.UID,
		Allowed: true,
		Result:  &metav1.Status{},
	}

	defer func() {
		responseBytes, err := json.Marshal(reviewResponse)
		if err != nil {
			logger.Info(fmt.Sprintf("failed converting final response.body into valid JSON: %s", err.Error()))
			return
		}

		response.WriteHeader(http.StatusOK)
		response.Header().Set("Content-Type", "application/json")

		_, err = response.Write(responseBytes)
		if err != nil {
			logger.Info(fmt.Sprintf("failed writing response to the client: %s", err.Error()))
		}
	}()

	// Craft the resourcePattern to look for the ClusterMutationPolicy objects in the pool
	resourcePattern := fmt.Sprintf("%s/%s/%s/%s",
		requestObj.Request.Resource.Group,
		requestObj.Request.Resource.Version,
		requestObj.Request.Resource.Resource,
		requestObj.Request.Operation)

	// Create an object that will be injected in conditions/message
	// in later template evaluation stage
	commonTemplateInjectedObject := template.NewInjectedData()

	// Store data for later: operation, old+current object
	err = s.populatePolicyDataFromAdmission(&requestObj, commonTemplateInjectedObject)
	if err != nil {
		logger.Info(fmt.Sprintf("failed extracting data from AdmissionReview: %s", err.Error()))
		return
	}

	// Loop over ClusterMutationPolicy objects collecting the patches to apply.
	// Patches are calculated incrementally, applying patched over the previous patched object.
	// At this point, some extra params will be added to the object that will be injected in template
	jsonPatchOperations := jsondiff.Patch{}
	patchedObjectBytes := requestObj.Request.Object.Raw

	cmPolicyList := s.dependencies.ClusterMutationPolicyRegistry.Store.GetResources(resourcePattern)
	for _, cmPolicyObj := range cmPolicyList {

		// Automatically add some information to the logs
		started := time.Now()

		specificTemplateInjectedObject := *commonTemplateInjectedObject
		prepared := s.dependencies.Evaluator.Prepare(evaluator.Request{
			Policy:     cmPolicyObj,
			Conditions: cmPolicyObj.Spec.Conditions,
		}, &specificTemplateInjectedObject)

		result := s.mutateWith(cmPolicyObj, &specificTemplateInjectedObject, patchedObjectBytes, prepared)

		s.dependencies.Metrics.Evaluation(telemetry.KindMutate, cmPolicyObj.Name,
			time.Since(started), countSourceObjects(specificTemplateInjectedObject.Sources))
		s.dependencies.Metrics.Decision(telemetry.Decision{
			Kind:      telemetry.KindMutate,
			Policy:    cmPolicyObj.Name,
			Operation: string(requestObj.Request.Operation),
			Resource:  resourceID(requestObj.Request.Resource),
			Outcome:   result.Outcome,
			Reason:    result.Reason,
		})

		telemetry.LogDecision(logger, telemetry.DecisionLine{
			Policy:    cmPolicyObj.Name,
			Kind:      telemetry.KindMutate,
			Object:    objectRef,
			Operation: string(requestObj.Request.Operation),
			Result:    result.Outcome,
			Reason:    result.Reason,
			Err:       result.Err,
		}, "duration", time.Since(started).String())

		if result.Warning != "" {
			addWarning(reviewResponse.Response, result.Warning)
		}

		// Conditions are not met or the patch failed, keep the object as it was
		if result.Outcome != telemetry.OutcomePatched {
			continue
		}

		patchedObjectBytes = result.Patched
		jsonPatchOperations = append(jsonPatchOperations, result.Operations...)
	}

	// All working mutation patches are collected from policies, send them to Kubernetes
	jsonPatchOperationBytes, err := json.Marshal(jsonPatchOperations)
	if err != nil {
		logger.Info(fmt.Sprintf("failed encoding the patch: %s", err.Error()))
		return
	}

	reviewResponse.Response.Patch = jsonPatchOperationBytes
	patchType := admissionv1.PatchTypeJSONPatch
	reviewResponse.Response.PatchType = &patchType
}

// mutationResult is what one policy did with the object
type mutationResult struct {
	// Patched is the object after applying the patch of the policy
	Patched []byte

	// Operations are the operations to send to Kubernetes
	Operations jsondiff.Patch

	Outcome telemetry.Outcome
	Reason  telemetry.Reason

	// Err is what went wrong, for the logs
	Err error

	// Warning is a message for the user, empty when there is nothing to say
	Warning string
}

// mutateWith applies the patch of one policy to the object, when its conditions were met
func (s *HttpServer) mutateWith(
	policy *v1alpha1.ClusterMutationPolicy,
	data *template.InjectedData,
	object []byte,
	prepared evaluator.Result,
) mutationResult {

	if !prepared.Passed {
		if prepared.Reason == telemetry.ReasonConditions {
			return mutationResult{
				Patched: object,
				Outcome: telemetry.OutcomeFailed,
				Reason:  telemetry.ReasonConditions,
				Err:     prepared.Err,
				Warning: fmt.Sprintf("admitik: policy '%s' could not evaluate its conditions, so it was skipped", policy.Name),
			}
		}

		return mutationResult{Patched: object, Outcome: telemetry.OutcomeSkipped, Reason: prepared.Reason}
	}

	// A broken source does not stop the policy, but the user has to know
	warning := ""
	if prepared.Reason == telemetry.ReasonSources {
		warning = fmt.Sprintf(
			"admitik: policy '%s' could not read its sources, so it was applied with the objects available",
			policy.Name)
	}

	parsedPatch, err := s.dependencies.Engines.Evaluate(policy.Spec.Patch.Engine, policy.Spec.Patch.Template, data)
	if err != nil {
		return mutationResult{
			Patched: object,
			Outcome: telemetry.OutcomeFailed,
			Reason:  telemetry.ReasonTemplate,
			Err:     err,
			Warning: fmt.Sprintf("admitik: policy '%s' could not render its patch, so the object was not mutated by it", policy.Name),
		}
	}

	operations, patched, err := s.generateJsonPatchOperations(object, policy.Spec.Patch.Type, []byte(parsedPatch))
	if err != nil {
		return mutationResult{
			Patched: object,
			Outcome: telemetry.OutcomeFailed,
			Reason:  telemetry.ReasonPatch,
			Err:     err,
			Warning: fmt.Sprintf("admitik: policy '%s' generated an invalid patch, so the object was not mutated by it", policy.Name),
		}
	}

	return mutationResult{
		Patched:    patched,
		Operations: operations,
		Outcome:    telemetry.OutcomePatched,
		Reason:     prepared.Reason,
		Warning:    warning,
	}
}

// generateJsonPatchOperations return a group of JsonPatch operations to mutate an object from its original
// state to a final state. It's compatible with 'jsonpatch', 'jsonmerge' and 'strategicmerge' patch types.
func (s *HttpServer) generateJsonPatchOperations(objectToPatch []byte, patchType string, patch []byte) (jsonPatchOperations jsondiff.Patch, patchedObject []byte, err error) {
	logger := log.FromContext(*s.dependencies.Context).
		WithValues("controller", "admissionserver", "action", "mutation")

	var patchedObjectBytes []byte
	patchType = strings.ToLower(patchType)

	// Apply user-defined patch to the entering object
	switch patchType {
	case v1alpha1.MutationPatchTypeMerge:
		patchedObjectBytes, err = s.generateJsonMergePatch(objectToPatch, patch)
		if err != nil {
			return nil, nil, fmt.Errorf("jsonmerge patch failed: %v", err)
		}
	case v1alpha1.MutationPatchTypeStrategicMerge:
		patchedObjectBytes, err = s.generateStrategicMergePatch(objectToPatch, patch)
		if err != nil {
			return nil, nil, fmt.Errorf("strategicmerge patch failed: %v", err)
		}
	default:
		patchedObjectBytes, err = s.generateJsonPatchPatch(objectToPatch, patch)
		if err != nil {
			return nil, nil, fmt.Errorf("jsonpatch patch failed: %v", err)
		}
	}

	// Calculate the difference for Kubernetes API
	// Store only successful operations in the operations list
	var tmpJsonPatchOperations jsondiff.Patch

	tmpJsonPatchOperations, err = jsondiff.CompareJSON(objectToPatch, patchedObjectBytes)
	if err != nil {
		return nil, nil, err
	}

	// DEBUG: log candidate object, already patched candidate for it and operations that will be applied.
	// This is useful to identify issues in patching engines.
	logger.V(5).Info("generated patch",
		"type", patchType,
		"candidate", string(objectToPatch),
		"patchedCandidate", string(patchedObjectBytes),
		"jsonPatchOperations", tmpJsonPatchOperations.String(),
	)

	return tmpJsonPatchOperations, patchedObjectBytes, nil
}

// generateJsonPatchPatch patches the object using JSONPath strategy and returns the resulting object
// Patch must be expressed in JSON
func (s *HttpServer) generateJsonPatchPatch(objectToPatch []byte, patch []byte) (patchedObjectBytes []byte, err error) {

	var tmpPatch jsonpatch.Patch
	tmpPatch, err = jsonpatch.DecodePatch(patch)
	if err != nil {
		return nil, err
	}

	patchedObjectBytes, err = tmpPatch.Apply(objectToPatch)
	if err != nil {
		return nil, err
	}

	return patchedObjectBytes, nil
}

// generateJsonMergePatch patches the object using JSONMerge strategy and returns the resulting object
// Patch must be expressed in YAML
func (s *HttpServer) generateJsonMergePatch(objectToPatch []byte, patch []byte) (patchedObjectBytes []byte, err error) {

	var tmpPatchBytes []byte
	tmpPatchBytes, err = yaml.YAMLToJSON(patch)
	if err != nil {
		return nil, err
	}

	patchedObjectBytes, err = jsonpatch.MergePatch(objectToPatch, tmpPatchBytes)
	if err != nil {
		return nil, err
	}

	return patchedObjectBytes, nil
}

// generateStrategicMergePatch patches the object using StrategicMerge strategy and returns the resulting object
// Patch must be expressed in YAML
// TODO: Could we use official Kubernetes package for this?
// Ref: https://github.com/kubernetes/kubernetes/blob/bd715a38d32561b45742b2d0cf0762b024107a31/cmd/kubeadm/app/util/patches/patches.go#L211-L216
// Ref: SMP using OpenAPI schemas like this project: https://github.com/kubernetes/kubernetes/blob/bd715a38d32561b45742b2d0cf0762b024107a31/staging/src/k8s.io/apimachinery/pkg/util/strategicpatch/patch.go#L821
func (s *HttpServer) generateStrategicMergePatch(objectToPatch []byte, patch []byte) (patchedObjectBytes []byte, err error) {

	var tmpPatchBytes []byte
	tmpPatchBytes, err = yaml.YAMLToJSON(patch)
	if err != nil {
		return nil, err
	}

	//
	tmpPatchObject := map[string]any{}
	err = json.Unmarshal(tmpPatchBytes, &tmpPatchObject)
	if err != nil {
		return nil, err
	}

	tmpOriginalObject := map[string]any{}
	err = json.Unmarshal(objectToPatch, &tmpOriginalObject)
	if err != nil {
		return nil, err
	}

	// Take original GVK as default. Overwrite it when the patch specifies another.
	desiredGvk, err := globals.GetObjectGVK(&tmpOriginalObject)
	if err != nil {
		return nil, err
	}

	patchGVK, err := globals.GetObjectGVK(&tmpPatchObject)
	if err == nil {
		desiredGvk = patchGVK
	}

	//
	initialSchema := s.strategicMergePatcher.GetSchemaByGVK(schema.GroupVersionKind{
		Group:   desiredGvk.Group,
		Version: desiredGvk.Version,
		Kind:    desiredGvk.Kind,
	})

	patchedObject, err := s.strategicMergePatcher.StrategicMerge(tmpOriginalObject, tmpPatchObject, initialSchema)
	if err != nil {
		return nil, err
	}

	patchedObjectBytes, err = json.Marshal(patchedObject)
	if err != nil {
		return nil, err
	}

	return patchedObjectBytes, nil
}
