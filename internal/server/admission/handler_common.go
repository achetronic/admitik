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

	//
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
	"github.com/admitikcore/admitik/internal/common"
	"github.com/admitikcore/admitik/internal/template"
)

// populatePolicyDataFromAdmission enriches the policy evaluation context with data from the admission request.
// Extracts data such as the operation type, target object or previous object state (for updates) from the AdmissionReview
// and populates the corresponding fields in the policy evaluation data structure.
func (s *HttpServer) populatePolicyDataFromAdmission(adReview *admissionv1.AdmissionReview, injectedData *template.InjectedData) (err error) {

	// Store desired operation
	injectedData.Operation = common.GetNormalizedOperation(adReview.Request.Operation)

	// Store the previous object on updates
	if injectedData.Operation == common.NormalizedOperationUpdate {
		err = json.Unmarshal(adReview.Request.OldObject.Raw, &injectedData.OldObject)
		if err != nil {
			return fmt.Errorf("failed decoding JSON field 'request.oldObject': %s", err.Error())
		}
	}

	// Store the object that is being touched
	err = json.Unmarshal(adReview.Request.Object.Raw, &injectedData.Object)
	if err != nil {
		return fmt.Errorf("failed decoding JSON field 'request.object': %s", err.Error())
	}

	return nil
}

// maxWarningLength is the size a warning can have before Kubernetes truncates it
const maxWarningLength = 200

// addWarning adds a message the user will see in the output of kubectl. It is the channel used to
// tell why a policy did not do what the user expected, without writing anything to the cluster
func addWarning(response *admissionv1.AdmissionResponse, message string) {

	if len(message) > maxWarningLength {
		message = message[:maxWarningLength] + "..."
	}

	response.Warnings = append(response.Warnings, message)
}

// countSourceObjects returns how many source objects are injected in one evaluation
func countSourceObjects(sources map[int][]map[string]any) int {

	total := 0
	for _, objects := range sources {
		total += len(objects)
	}

	return total
}

// resourceID returns the resource as {group}/{version}/{resource}
func resourceID(resource metav1.GroupVersionResource) string {
	return common.ResourceLabel(resource.Group, resource.Version, resource.Resource)
}

// matchesScopes tells whether one of the intercepted resources of a policy covers the object.
// A scope with an empty namespace or name covers every one of them
func matchesScopes(
	scopes []v1alpha1.AdmissionResourceGroupT,
	resource metav1.GroupVersionResource,
	namespace, name string,
) bool {

	for _, scope := range scopes {

		if scope.Group != resource.Group ||
			scope.Version != resource.Version ||
			scope.Resource != resource.Resource {
			continue
		}

		if scope.Namespace != "" && scope.Namespace != namespace {
			continue
		}

		if scope.Name != "" && scope.Name != name {
			continue
		}

		return true
	}

	return false
}
