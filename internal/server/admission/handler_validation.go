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
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/common"
	"github.com/freepik-company/admitik/internal/telemetry"
	"github.com/freepik-company/admitik/internal/template"
)

// handleValidationRequest handles the incoming validation requests
func (s *HttpServer) handleValidationRequest(response http.ResponseWriter, request *http.Request) {
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

	// Automatically add some information to the logs
	logger = logger.WithValues(
		"object", fmt.Sprintf("%s/%s/%s/%s",
			requestObj.Request.Resource.Group,
			requestObj.Request.Resource.Version,
			requestObj.Request.Resource.Resource,
			requestObj.Request.Name,
		),
		"namespace", requestObj.Request.Namespace,
		"operation", requestObj.Request.Operation)

	logger.Info("object under validation review")

	// Assume that the request is rejected as default
	reviewResponse := &requestObj
	reviewResponse.Response = &admissionv1.AdmissionResponse{
		UID:     requestObj.Request.UID,
		Allowed: false,
		Result: &metav1.Status{
			Code: http.StatusForbidden,
		},
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

	// Craft the resourcePattern to look for the ClusterValidationPolicy objects in the pool
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

	// Loop over ClusterValidationPolicy resources performing actions
	// At this point, some extra params will be added to the object that will be injected in template
	caPolicyList := s.dependencies.ClusterValidationPolicyRegistry.Store.GetResources(resourcePattern)
	for _, caPolicyObj := range caPolicyList {

		// Automatically add some information to the logs
		policyLogger := logger.WithValues("ClusterValidationPolicy", caPolicyObj.Name)
		started := time.Now()

		// Retrieve the sources declared per policy
		fetchedSources, fetchErr := common.FetchPolicySources(s.dependencies.Engines, s.dependencies.Pool, caPolicyObj, commonTemplateInjectedObject)
		if fetchErr != nil {
			policyLogger.Info("failed fetching sources. Broken ones will be empty", "error", fetchErr.Error())
		}

		specificTemplateInjectedObject := *commonTemplateInjectedObject
		specificTemplateInjectedObject.Sources = fetchedSources

		result := s.validateWith(caPolicyObj, &specificTemplateInjectedObject)
		if fetchErr != nil {
			result.Warning = fmt.Sprintf(
				"admitik: policy '%s' could not read its sources, so it was evaluated with the objects available",
				caPolicyObj.Name)
			result.Reason = telemetry.ReasonSources
		}

		s.dependencies.Metrics.Evaluation(telemetry.KindValidate, caPolicyObj.Name,
			time.Since(started), countSourceObjects(fetchedSources))
		s.dependencies.Metrics.Decision(telemetry.Decision{
			Kind:      telemetry.KindValidate,
			Policy:    caPolicyObj.Name,
			Operation: string(requestObj.Request.Operation),
			Resource:  resourceID(requestObj.Request.Resource),
			Outcome:   result.Outcome,
			Reason:    result.Reason,
		})

		if result.Warning != "" {
			policyLogger.Info(result.Warning)
			addWarning(reviewResponse.Response, result.Warning)
		}

		// Conditions are met, this policy does not reject the object
		if !result.Denied {
			continue
		}

		reviewResponse.Response.Allowed = result.Permissive
		reviewResponse.Response.Result.Message = result.Message

		if result.Permissive {
			addWarning(reviewResponse.Response, fmt.Sprintf(
				"admitik: policy '%s' is in permissive mode, this object violates it", caPolicyObj.Name))
			policyLogger.Info(fmt.Sprintf("object accepted with unmet conditions: %s", result.Message))
			continue
		}

		policyLogger.Info(fmt.Sprintf("object rejected due to unmet conditions: %s", result.Message))
		return
	}

	reviewResponse.Response.Allowed = true
	reviewResponse.Response.Result = &metav1.Status{}
}

// validationResult is what one policy decided about the object
type validationResult struct {
	// Denied is whether the policy rejects the object
	Denied bool

	// Permissive is whether the policy only reports the violation
	Permissive bool

	// Message is the reason shown to the user
	Message string

	Outcome telemetry.Outcome
	Reason  telemetry.Reason

	// Warning is a message for the user, empty when there is nothing to say
	Warning string
}

// validateWith evaluates the conditions and the message of one policy over the object
func (s *HttpServer) validateWith(
	policy *v1alpha1.ClusterValidationPolicy,
	data *template.InjectedData,
) validationResult {

	permissive := strings.ToLower(policy.Spec.FailureAction) == v1alpha1.ValidationFailureActionPermissive

	conditionsPassed, condErr := common.IsPassingConditions(s.dependencies.Engines, policy.Spec.Conditions, data)

	result := validationResult{}
	if condErr != nil {
		result.Warning = fmt.Sprintf("admitik: policy '%s' could not evaluate its conditions", policy.Name)
		result.Reason = telemetry.ReasonConditions
	}

	if conditionsPassed {
		result.Outcome = telemetry.OutcomeAllowed
		return result
	}

	// A policy that cannot be evaluated rejects the object, unless it is in permissive mode
	result.Denied = true
	result.Permissive = permissive
	result.Outcome = telemetry.OutcomeDenied
	if permissive {
		result.Outcome = telemetry.OutcomePermissive
	}
	if condErr != nil {
		result.Outcome = telemetry.OutcomeFailed
	}

	message, err := s.dependencies.Engines.Evaluate(policy.Spec.Message.Engine, policy.Spec.Message.Template, data)
	if err != nil {
		result.Message = "Reason unavailable: message template failed. More info in controller logs."
		result.Warning = fmt.Sprintf("admitik: policy '%s' could not render its message", policy.Name)
		result.Reason = telemetry.ReasonTemplate
		return result
	}

	result.Message = message
	return result
}
