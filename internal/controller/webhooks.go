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

package controller

import (
	"fmt"
	"slices"
	"strings"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// webhookRuleScopeAll makes a rule apply to namespaced and cluster scoped objects
var webhookRuleScopeAll = admissionregv1.ScopeType("*")

// WebhookOptions is everything a webhook needs, whatever its kind
type WebhookOptions struct {
	// Name of the webhook inside the configuration
	Name string

	// Rules of the webhook, one per watched type
	Rules []admissionregv1.RuleWithOperations

	// ClientConfig is how the API server reaches the admission server
	ClientConfig admissionregv1.WebhookClientConfig

	// TimeoutSeconds is how long the API server waits for the answer
	TimeoutSeconds int32

	// CurrentNamespace is the namespace where Admitik runs
	CurrentNamespace string

	// ExcludeSelfNamespace leaves the namespace of Admitik out of admission
	ExcludeSelfNamespace bool

	// ExcludedNamespaces is a comma separated list of namespaces to leave out
	ExcludedNamespaces string

	// IgnoreLabeledObjects leaves out the objects carrying the ignore label
	IgnoreLabeledObjects bool
}

// NewMutatingWebhook builds a mutating webhook. A mutating and a validating webhook are different
// types with the same fields, so there is one constructor per kind and the rules and the selectors
// are built in one place
func NewMutatingWebhook(options WebhookOptions) admissionregv1.MutatingWebhook {

	timeoutSeconds := options.TimeoutSeconds
	sideEffects := admissionregv1.SideEffectClassNone

	return admissionregv1.MutatingWebhook{
		Name:                    options.Name,
		AdmissionReviewVersions: []string{"v1"},
		ClientConfig:            options.ClientConfig,
		Rules:                   options.Rules,
		TimeoutSeconds:          &timeoutSeconds,
		NamespaceSelector:       excludedNamespacesSelector(options),
		ObjectSelector:          ignoredObjectsSelector(options),
		SideEffects:             &sideEffects,
	}
}

// NewValidatingWebhook builds a validating webhook
func NewValidatingWebhook(options WebhookOptions) admissionregv1.ValidatingWebhook {

	timeoutSeconds := options.TimeoutSeconds
	sideEffects := admissionregv1.SideEffectClassNone

	return admissionregv1.ValidatingWebhook{
		Name:                    options.Name,
		AdmissionReviewVersions: []string{"v1"},
		ClientConfig:            options.ClientConfig,
		Rules:                   options.Rules,
		TimeoutSeconds:          &timeoutSeconds,
		NamespaceSelector:       excludedNamespacesSelector(options),
		ObjectSelector:          ignoredObjectsSelector(options),
		SideEffects:             &sideEffects,
	}
}

// WebhookRules returns one rule per watched type, sorted and without duplicates, so the same
// policies always produce the same configuration
func WebhookRules(watchedTypes []string) ([]admissionregv1.RuleWithOperations, error) {

	patterns := slices.Clone(watchedTypes)
	slices.Sort(patterns)
	patterns = slices.Compact(patterns)

	rules := make([]admissionregv1.RuleWithOperations, 0, len(patterns))
	for _, pattern := range patterns {

		parts := strings.Split(pattern, "/")
		if len(parts) != 4 {
			return nil, fmt.Errorf("invalid watched type '%s': expected {group}/{version}/{resource}/{operation}", pattern)
		}

		rules = append(rules, admissionregv1.RuleWithOperations{
			Rule: admissionregv1.Rule{
				APIGroups:   []string{parts[0]},
				APIVersions: []string{parts[1]},
				Resources:   []string{parts[2]},
				Scope:       &webhookRuleScopeAll,
			},
			Operations: []admissionregv1.OperationType{admissionregv1.OperationType(parts[3])},
		})
	}

	return rules, nil
}

// excludedNamespacesSelector leaves out the namespaces that must not be evaluated
func excludedNamespacesSelector(options WebhookOptions) *metav1.LabelSelector {

	excluded := []string{}
	if options.ExcludedNamespaces != "" {
		excluded = strings.Split(options.ExcludedNamespaces, ",")
	}
	if options.ExcludeSelfNamespace {
		excluded = append(excluded, options.CurrentNamespace)
	}

	if len(excluded) == 0 {
		return nil
	}

	return &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key:      "kubernetes.io/metadata.name",
		Operator: metav1.LabelSelectorOpNotIn,
		Values:   excluded,
	}}}
}

// ignoredObjectsSelector leaves out the objects that asked to be ignored
func ignoredObjectsSelector(options WebhookOptions) *metav1.LabelSelector {

	if !options.IgnoreLabeledObjects {
		return nil
	}

	return &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key:      IgnoreAdmissionLabel,
		Operator: metav1.LabelSelectorOpDoesNotExist,
	}}}
}
