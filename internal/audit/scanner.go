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
	"fmt"
	"slices"
	"strings"
	"time"

	//
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/evaluator"
	"github.com/freepik-company/admitik/internal/telemetry"
	"github.com/freepik-company/admitik/internal/template"
)

const (
	// ReportName is the name of the object holding the report of the last scan
	ReportName = "admitik-audit"

	// DefaultMaxSamples is how many non compliant objects are kept per result
	DefaultMaxSamples = 5

	// pageSize is how many objects are requested at a time
	pageSize = 500

	// auditOperation is the operation used to evaluate an object that already exists.
	// A CREATE makes the policies that only act on creation evaluate the object
	auditOperation = "CREATE"
)

type ScannerOptions struct {
	// MaxSamples is how many non compliant objects are kept per result
	MaxSamples int
}

type ScannerDependencies struct {
	// Client reads the policies and writes the report
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
		options.MaxSamples = DefaultMaxSamples
	}

	return &Scanner{options: options, dependencies: dependencies}
}

// Scan evaluates every validation policy over the objects of the resources it intercepts and
// writes one aggregated report
func (s *Scanner) Scan(ctx context.Context) (*v1alpha1.ClusterAuditReport, error) {

	logger := log.FromContext(ctx).WithValues("task", "audit")
	started := time.Now()

	policies := &v1alpha1.ClusterValidationPolicyList{}
	if err := s.dependencies.Client.List(ctx, policies); err != nil {
		return nil, fmt.Errorf("error listing the ClusterValidationPolicy resources: %w", err)
	}

	report := &v1alpha1.ClusterAuditReport{}
	report.Name = ReportName
	report.Status.ScanTime = metav1.NewTime(started)

	for index := range policies.Items {
		policy := &policies.Items[index]

		for _, resource := range interceptedResources(policy) {
			result, err := s.scanResource(ctx, logger, policy, resource)
			if err != nil {
				logger.Info("failed scanning a resource", "policy", policy.Name, "resource", result.Resource,
					"error", err.Error())
				result.Errors++
			}

			report.Status.Results = append(report.Status.Results, result)
			report.Status.Summary.Scanned += result.Scanned
			report.Status.Summary.Compliant += result.Compliant
			report.Status.Summary.NonCompliant += result.NonCompliant
			report.Status.Summary.Errors += result.Errors
		}

		report.Status.Summary.Policies++
	}

	report.Status.Duration = time.Since(started).Round(time.Millisecond).String()

	if err := s.store(ctx, report); err != nil {
		return nil, err
	}

	return report, nil
}

// scanResource evaluates one policy over the objects of one resource type
func (s *Scanner) scanResource(
	ctx context.Context,
	logger interface {
		Info(msg string, keysAndValues ...any)
	},
	policy *v1alpha1.ClusterValidationPolicy,
	resource schema.GroupVersionResource,
) (v1alpha1.AuditResult, error) {

	result := v1alpha1.AuditResult{
		Policy:   policy.Name,
		Kind:     "ClusterValidationPolicy",
		Resource: strings.Join([]string{resource.Group, resource.Version, resource.Resource}, "/"),
	}

	objects, err := s.listObjects(ctx, resource)
	if err != nil {
		return result, err
	}

	for _, object := range objects {
		result.Scanned++

		data := template.NewInjectedData()
		data.Operation = auditOperation
		data.Object = object

		// The sources are only there when another policy is already watching them
		prepared := s.dependencies.Evaluator.Prepare(evaluator.Request{
			Policy:     policy,
			Conditions: policy.Spec.Conditions,
		}, data)

		if prepared.Reason == telemetry.ReasonConditions {
			result.Errors++
			logger.Info("failed evaluating conditions during the audit",
				"policy", policy.Name, "object", objectName(object), "error", prepared.Err.Error())
			continue
		}
		if prepared.Err != nil {
			logger.Info("failed fetching sources for the audit",
				"policy", policy.Name, "object", objectName(object), "error", prepared.Err.Error())
		}

		if prepared.Passed {
			result.Compliant++
			continue
		}

		result.NonCompliant++
		if len(result.Samples) < s.options.MaxSamples {
			result.Samples = append(result.Samples, objectName(object))
		}
	}

	return result, nil
}

// listObjects returns every object of a resource type, paging through the results
func (s *Scanner) listObjects(ctx context.Context, resource schema.GroupVersionResource) ([]map[string]any, error) {

	objects := []map[string]any{}
	options := metav1.ListOptions{Limit: pageSize}

	for {
		list, err := s.dependencies.Dynamic.Resource(resource).List(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("error listing %s: %w", resource, err)
		}

		for index := range list.Items {
			objects = append(objects, list.Items[index].Object)
		}

		if list.GetContinue() == "" {
			return objects, nil
		}

		options.Continue = list.GetContinue()
	}
}

// store writes the report, creating it the first time
func (s *Scanner) store(ctx context.Context, report *v1alpha1.ClusterAuditReport) error {

	existing := &v1alpha1.ClusterAuditReport{}

	err := s.dependencies.Client.Get(ctx, types.NamespacedName{Name: report.Name}, existing)
	if apierrors.IsNotFound(err) {
		if err := s.dependencies.Client.Create(ctx, report); err != nil {
			return fmt.Errorf("error creating the audit report: %w", err)
		}

		existing = report
	} else if err != nil {
		return fmt.Errorf("error getting the audit report: %w", err)
	}

	// The status is a subresource, so it is not written on creation
	existing.Status = report.Status
	if err := s.dependencies.Client.Status().Update(ctx, existing); err != nil {
		return fmt.Errorf("error updating the audit report: %w", err)
	}

	return nil
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

		if slices.Contains(resources, resource) {
			continue
		}

		resources = append(resources, resource)
	}

	return resources
}

// objectName returns the object as namespace/name
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
