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
	"slices"
	"testing"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestWebhookRules(t *testing.T) {
	rules, err := WebhookRules([]string{
		"/v1/namespaces/UPDATE",
		"apps/v1/deployments/CREATE",
		"/v1/namespaces/UPDATE",
		"/v1/namespaces/CREATE",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rules) != 3 {
		t.Fatalf("got %d rules, expected 3 without duplicates", len(rules))
	}

	got := []string{}
	for _, rule := range rules {
		got = append(got, rule.Resources[0]+"/"+string(rule.Operations[0]))
	}

	expected := []string{"namespaces/CREATE", "namespaces/UPDATE", "deployments/CREATE"}
	if !slices.Equal(got, expected) {
		t.Errorf("got %v, expected %v sorted", got, expected)
	}
}

func TestWebhookRulesWithAnInvalidType(t *testing.T) {
	if _, err := WebhookRules([]string{"/v1/namespaces"}); err == nil {
		t.Error("expected an error for a watched type without operation")
	}
}

func TestWebhookRulesWithoutWatchedTypes(t *testing.T) {
	rules, err := WebhookRules(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rules) != 0 {
		t.Errorf("got %v, expected no rules", rules)
	}
}

func TestWebhookFieldsAreTheSameForBothKinds(t *testing.T) {
	options := WebhookOptions{
		Name:                 "test.admitik.svc",
		ClientConfig:         admissionregv1.WebhookClientConfig{Service: &admissionregv1.ServiceReference{Name: "svc"}},
		TimeoutSeconds:       12,
		CurrentNamespace:     "admitik-system",
		ExcludeSelfNamespace: true,
		ExcludedNamespaces:   "kube-system,kube-public",
		IgnoreLabeledObjects: true,
	}

	mutating := NewMutatingWebhook(options)
	validating := NewValidatingWebhook(options)

	if mutating.Name != validating.Name {
		t.Errorf("got different names: %q and %q", mutating.Name, validating.Name)
	}
	if *mutating.TimeoutSeconds != *validating.TimeoutSeconds {
		t.Errorf("got different timeouts: %d and %d", *mutating.TimeoutSeconds, *validating.TimeoutSeconds)
	}
	if *mutating.SideEffects != *validating.SideEffects {
		t.Errorf("got different side effects")
	}
	if !slices.Equal(mutating.AdmissionReviewVersions, validating.AdmissionReviewVersions) {
		t.Error("got different admission review versions")
	}
	if mutating.NamespaceSelector.String() != validating.NamespaceSelector.String() {
		t.Error("got different namespace selectors")
	}
	if mutating.ObjectSelector.String() != validating.ObjectSelector.String() {
		t.Error("got different object selectors")
	}
}

func TestWebhookLeavesOutTheNamespacesAndTheIgnoredObjects(t *testing.T) {
	webhook := NewMutatingWebhook(WebhookOptions{
		TimeoutSeconds:       10,
		CurrentNamespace:     "admitik-system",
		ExcludeSelfNamespace: true,
		ExcludedNamespaces:   "kube-system",
		IgnoreLabeledObjects: true,
	})

	namespaces := webhook.NamespaceSelector.MatchExpressions
	if len(namespaces) != 1 || namespaces[0].Operator != metav1.LabelSelectorOpNotIn {
		t.Fatalf("got %v, expected one NotIn expression", namespaces)
	}
	if !slices.Equal(namespaces[0].Values, []string{"kube-system", "admitik-system"}) {
		t.Errorf("got %v, expected kube-system and the namespace of Admitik", namespaces[0].Values)
	}

	objects := webhook.ObjectSelector.MatchExpressions
	if len(objects) != 1 || objects[0].Key != IgnoreAdmissionLabel {
		t.Fatalf("got %v, expected the ignore label expression", objects)
	}
	if objects[0].Operator != metav1.LabelSelectorOpDoesNotExist {
		t.Errorf("got %q, expected DoesNotExist", objects[0].Operator)
	}
}

func TestWebhookWithoutExclusionsHasNoSelectors(t *testing.T) {
	webhook := NewValidatingWebhook(WebhookOptions{TimeoutSeconds: 10})

	if webhook.NamespaceSelector != nil {
		t.Errorf("got %v, expected no namespace selector", webhook.NamespaceSelector)
	}
	if webhook.ObjectSelector != nil {
		t.Errorf("got %v, expected no object selector", webhook.ObjectSelector)
	}
}

func TestWebhookDefaults(t *testing.T) {
	webhook := NewValidatingWebhook(WebhookOptions{TimeoutSeconds: 10})

	if !slices.Equal(webhook.AdmissionReviewVersions, []string{"v1"}) {
		t.Errorf("got %v, expected v1", webhook.AdmissionReviewVersions)
	}
	if webhook.SideEffects == nil || *webhook.SideEffects != admissionregv1.SideEffectClassNone {
		t.Errorf("got %v, expected None as side effects", webhook.SideEffects)
	}
	if webhook.TimeoutSeconds == nil || *webhook.TimeoutSeconds != 10 {
		t.Errorf("got %v, expected 10 seconds", webhook.TimeoutSeconds)
	}
}
