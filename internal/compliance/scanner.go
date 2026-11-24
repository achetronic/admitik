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
	"fmt"
	"strings"
	"time"

	//
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
	"github.com/admitikcore/admitik/internal/common"
	"github.com/admitikcore/admitik/internal/evaluator"
	"github.com/admitikcore/admitik/internal/telemetry"
	"github.com/admitikcore/admitik/internal/template"
)

const (
	// defaultMaxSamples is how many non compliant objects are named per result
	defaultMaxSamples = 5

	// defaultPageSize is how many objects are requested at a time
	defaultPageSize = 500

	// scanOperation is the operation used to evaluate an object that already exists.
	// A CREATE makes the policies that only act on creation evaluate the object
	scanOperation = "CREATE"
)

// Result is what one policy found over the objects of one resource type
type Result struct {
	Policy   string
	Resource string

	Scanned      int
	Compliant    int
	NonCompliant int
	Errors       int

	// Samples are a few non compliant objects, as namespace/name
	Samples []string
}

// Summary aggregates what a scan found
type Summary struct {
	Policies     int
	Scanned      int
	Compliant    int
	NonCompliant int
	Errors       int
}

// Scan is the outcome of a pass over the objects that already exist
type Scan struct {
	Started  time.Time
	Duration time.Duration

	Summary Summary
	Results []Result
}

type ScannerOptions struct {
	// MaxSamples is how many non compliant objects are named per result
	MaxSamples int

	// PageSize is how many objects are requested at a time
	PageSize int64
}

type ScannerDependencies struct {
	// Client lists the policies
	Client client.Client

	// Dynamic lists the objects to evaluate
	Dynamic dynamic.Interface

	// Evaluator prepares the context of the policies and checks their conditions.
	// The sources are only there for the ones some policy is already watching
	Evaluator *evaluator.Evaluator
}

// Scanner evaluates the validation policies over the objects that already exist in the cluster
type Scanner struct {
	options      ScannerOptions
	dependencies ScannerDependencies
}

func NewScanner(options ScannerOptions, dependencies ScannerDependencies) *Scanner {

	if options.MaxSamples == 0 {
		options.MaxSamples = defaultMaxSamples
	}
	if options.PageSize == 0 {
		options.PageSize = defaultPageSize
	}

	return &Scanner{options: options, dependencies: dependencies}
}

// Scan evaluates every validation policy over the objects of the resources it intercepts
func (s *Scanner) Scan(ctx context.Context) (Scan, error) {

	logger := log.FromContext(ctx).WithValues("task", "compliance")
	started := time.Now()

	policies := &v1alpha1.ClusterValidationPolicyList{}
	if err := s.dependencies.Client.List(ctx, policies); err != nil {
		return Scan{}, fmt.Errorf("error listing the ClusterValidationPolicy resources: %w", err)
	}

	scan := Scan{Started: started, Results: []Result{}}

	for index := range policies.Items {
		policy := &policies.Items[index]

		for _, resource := range interceptedResources(policy) {
			result, err := s.scanResource(ctx, logger, policy, resource)
			if err != nil {
				logger.Error(err, "failed scanning a resource",
					"policy", policy.Name, "resource", result.Resource)
				result.Errors++
			}

			s.logResult(logger, result)

			scan.Results = append(scan.Results, result)
			scan.Summary.Scanned += result.Scanned
			scan.Summary.Compliant += result.Compliant
			scan.Summary.NonCompliant += result.NonCompliant
			scan.Summary.Errors += result.Errors
		}

		scan.Summary.Policies++
	}

	scan.Duration = time.Since(started).Round(time.Millisecond)

	return scan, nil
}

// scanResource evaluates one policy over the objects of one resource type, page by page
func (s *Scanner) scanResource(
	ctx context.Context,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
	policy *v1alpha1.ClusterValidationPolicy,
	resource schema.GroupVersionResource,
) (Result, error) {

	result := Result{
		Policy:   policy.Name,
		Resource: common.ResourceLabel(resource.Group, resource.Version, resource.Resource),
	}

	options := metav1.ListOptions{Limit: s.options.PageSize}

	for {
		list, err := s.dependencies.Dynamic.Resource(resource).List(ctx, options)
		if err != nil {
			return result, fmt.Errorf("error listing %s: %w", result.Resource, err)
		}

		for index := range list.Items {
			s.evaluateObject(logger, policy, list.Items[index].Object, &result)
		}

		if list.GetContinue() == "" {
			return result, nil
		}

		options.Continue = list.GetContinue()
	}
}

// evaluateObject evaluates one existing object and adds it to the result
func (s *Scanner) evaluateObject(
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
	policy *v1alpha1.ClusterValidationPolicy,
	object map[string]any,
	result *Result,
) {

	result.Scanned++

	data := template.NewInjectedData()
	data.Operation = scanOperation
	data.Object = object

	prepared := s.dependencies.Evaluator.Prepare(evaluator.Request{
		Policy:     policy,
		Conditions: policy.Spec.Conditions,
	}, data)

	if prepared.Err != nil {
		result.Errors++
		logger.Info("an object could not be evaluated during the compliance scan",
			"policy", policy.Name, "object", objectName(object), "reason", prepared.Reason,
			"error", prepared.Err.Error())
		return
	}

	if prepared.Passed {
		result.Compliant++
		return
	}

	result.NonCompliant++
	if len(result.Samples) < s.options.MaxSamples {
		result.Samples = append(result.Samples, objectName(object))
	}
}

// logResult tells what one policy found, naming a few of the objects that do not meet it
func (s *Scanner) logResult(
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
	result Result,
) {

	if result.NonCompliant == 0 && result.Errors == 0 {
		return
	}

	objects := strings.Join(result.Samples, ", ")
	if result.NonCompliant > len(result.Samples) {
		objects = fmt.Sprintf("%s and %d more", objects, result.NonCompliant-len(result.Samples))
	}

	logger.Info("objects do not meet a policy",
		"policy", result.Policy, "resource", result.Resource,
		"nonCompliant", result.NonCompliant, "errors", result.Errors, "objects", objects)
}

// objectName identifies an object as namespace/name, or just name when it is cluster scoped
func objectName(object map[string]any) string {

	metadata, isMap := object["metadata"].(map[string]any)
	if !isMap {
		return ""
	}

	name, _ := metadata["name"].(string)
	namespace, _ := metadata["namespace"].(string)

	if namespace == "" {
		return name
	}

	return namespace + "/" + name
}

// interceptedResources returns the resource types a policy intercepts, without duplicates
func interceptedResources(policy *v1alpha1.ClusterValidationPolicy) []schema.GroupVersionResource {

	resources := []schema.GroupVersionResource{}
	for _, intercepted := range policy.Spec.InterceptedResources {
		resource := schema.GroupVersionResource{
			Group:    intercepted.Group,
			Version:  intercepted.Version,
			Resource: intercepted.Resource,
		}

		if !containsResource(resources, resource) {
			resources = append(resources, resource)
		}
	}

	return resources
}

func containsResource(resources []schema.GroupVersionResource, resource schema.GroupVersionResource) bool {

	for _, candidate := range resources {
		if candidate == resource {
			return true
		}
	}

	return false
}

// ToTelemetry converts the results of a scan into what the metrics record
func ToTelemetry(results []Result) []telemetry.ComplianceResult {

	converted := make([]telemetry.ComplianceResult, 0, len(results))
	for _, result := range results {
		converted = append(converted, telemetry.ComplianceResult{
			Policy:       result.Policy,
			Resource:     result.Resource,
			Compliant:    result.Compliant,
			NonCompliant: result.NonCompliant,
			Errors:       result.Errors,
		})
	}

	return converted
}
