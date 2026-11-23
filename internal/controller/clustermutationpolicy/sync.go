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

package clustermutationpolicy

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	//
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/controller"
	"github.com/freepik-company/admitik/internal/pubsub"
)

const (

	//
	resourceUpdatedMessage  = "A ClusterMutationPolicy was modified: will be updated into the internal registry"
	resourceDeletionMessage = "A ClusterMutationPolicy was deleted: will be deleted from internal registry"
)

const (
	// MutatingWebhookConfigurationName represents the name of the MutatingWebhookConfiguration resource
	// that will be created or updated in Kubernetes to forward requests to the admissions webserver
	MutatingWebhookConfigurationName = "admitik-cluster-mutation-policy"
)

var (
	MutationOperations = []admissionregv1.OperationType{
		admissionregv1.Create,
		admissionregv1.Update,
		admissionregv1.Delete,
		admissionregv1.Connect}

	//
	MutatingWebhookConfigurationRuleScopeAll = admissionregv1.ScopeType("*")
)

// ReconcileClusterMutationPolicy keeps internal ClusterMutationPolicy resources' registry up-to-date
func (r *ClusterMutationPolicyReconciler) ReconcileClusterMutationPolicy(
	ctx context.Context, eventType watch.EventType, resourceManifest *v1alpha1.ClusterMutationPolicy) (err error) {

	logger := log.FromContext(ctx)

	// Publish the event in the registry event bus
	r.Dependencies.ClusterMutationPolicyRegistry.Broadcaster.Publish(pubsub.Event[*v1alpha1.ClusterMutationPolicy]{
		Type:   string(eventType),
		Object: resourceManifest,
	})

	watched := WatchedTypes(resourceManifest)

	// Update the registry store
	if eventType == watch.Deleted {
		for _, watchedType := range watched {
			logger.Info(resourceDeletionMessage, "watcher", watchedType)
			r.Dependencies.ClusterMutationPolicyRegistry.Store.RemoveResource(watchedType, resourceManifest)
		}
	} else {
		for _, watchedType := range watched {
			logger.Info(resourceUpdatedMessage, "watcher", watchedType)
			r.Dependencies.ClusterMutationPolicyRegistry.Store.AddOrUpdateResource(watchedType, resourceManifest)
		}
	}

	// Clean non-desired watched types. This is needed for updates where the user
	// reduces the amount of watched operations on watched resources
	for _, registeredResourceType := range r.Dependencies.ClusterMutationPolicyRegistry.Store.GetCollectionNames() {
		if !slices.Contains(watched, registeredResourceType) {
			r.Dependencies.ClusterMutationPolicyRegistry.Store.RemoveResource(registeredResourceType, resourceManifest)
		}
	}

	// Order each collection by priority, so the patches are always applied in the same order.
	// This is what makes the mutation deterministic between replicas
	for _, watchedType := range watched {
		r.Dependencies.ClusterMutationPolicyRegistry.Store.SortCollection(watchedType, func(a, b *v1alpha1.ClusterMutationPolicy) bool {
			return ComparePolicies(a, b) < 0
		})
	}

	// Only the leader writes the webhook configuration. The rest of the replicas keep the store
	// updated, which is what the admission server of each replica needs
	if !r.IsLeader() {
		return nil
	}

	return r.SyncWebhookConfiguration(ctx)
}

// WatchedTypes returns the collection keys the policy applies to, expressed as
// {group}/{version}/{resource}/{operation}
func WatchedTypes(policy *v1alpha1.ClusterMutationPolicy) []string {

	watched := []string{}

	for _, intercResourceGroup := range policy.Spec.InterceptedResources {

		operations := intercResourceGroup.Operations
		if slices.Contains(operations, admissionregv1.OperationAll) {
			operations = MutationOperations
		}

		for _, operation := range operations {

			if !slices.Contains(MutationOperations, operation) {
				continue
			}

			watchedType := strings.Join([]string{
				intercResourceGroup.Group,
				intercResourceGroup.Version,
				intercResourceGroup.Resource,
				string(operation),
			}, "/")

			if slices.Contains(watched, watchedType) {
				continue
			}

			watched = append(watched, watchedType)
		}
	}

	return watched
}

// ComparePolicies orders the policies by priority and, for the same priority, by name.
// The name is the tie breaker that makes the order total, so every replica gets the same one
func ComparePolicies(a, b *v1alpha1.ClusterMutationPolicy) int {
	return cmp.Or(
		cmp.Compare(a.Spec.Priority, b.Spec.Priority),
		strings.Compare(a.Name, b.Name),
	)
}

// SyncWebhookConfiguration builds the webhook configuration from the policies in the cluster and
// applies it. It must only be called by the leader
func (r *ClusterMutationPolicyReconciler) SyncWebhookConfiguration(ctx context.Context) error {

	metaWebhookObj, alreadyCreated, err := r.buildWebhookConfiguration(ctx)
	if err != nil {
		return fmt.Errorf("error building MutatingWebhookConfiguration '%s': %w", MutatingWebhookConfigurationName, err)
	}

	// Sync changes to Kubernetes
	if !alreadyCreated {
		if err = r.Create(ctx, &metaWebhookObj); err != nil {
			return fmt.Errorf("error creating MutatingWebhookConfiguration in Kubernetes '%s': %w",
				MutatingWebhookConfigurationName, err)
		}

		return nil
	}

	if err = r.Update(ctx, &metaWebhookObj); err != nil {
		return fmt.Errorf("error updating MutatingWebhookConfiguration in Kubernetes '%s': %w",
			MutatingWebhookConfigurationName, err)
	}

	return nil
}

// buildWebhookConfiguration returns the MutatingWebhookConfiguration the cluster should have,
// built from the policies that exist right now
func (r *ClusterMutationPolicyReconciler) buildWebhookConfiguration(ctx context.Context) (
	admissionregv1.MutatingWebhookConfiguration, bool, error) {

	metaWebhookObj := admissionregv1.MutatingWebhookConfiguration{}
	metaWebhookObj.Name = MutatingWebhookConfigurationName

	err := r.Get(ctx, types.NamespacedName{Name: MutatingWebhookConfigurationName}, &metaWebhookObj)
	alreadyCreated := err == nil
	if err != nil && !errors.IsNotFound(err) {
		return metaWebhookObj, false, fmt.Errorf("error getting the MutatingWebhookConfiguration '%s': %w",
			MutatingWebhookConfigurationName, err)
	}

	rules, err := r.webhookRules(ctx)
	if err != nil {
		return metaWebhookObj, alreadyCreated, err
	}

	// Create a bare new 'webhooks' section for the MutatingWebhookConfiguration and fill it
	tmpWebhookObj := admissionregv1.MutatingWebhook{}
	timeoutSecondsConverted := int32(r.Options.WebhookTimeout)

	tmpWebhookObj.Name = "mutate.admitik.svc"
	tmpWebhookObj.AdmissionReviewVersions = []string{"v1"}
	tmpWebhookObj.ClientConfig = r.Options.WebhookClientConfig
	tmpWebhookObj.Rules = rules
	tmpWebhookObj.TimeoutSeconds = &timeoutSecondsConverted

	// Ignore sensitive namespaces to avoid breaking Kubernetes essential services or chicken-egg scenarios
	selectedNamespaces := []string{}
	if !strings.EqualFold(r.Options.ExcludedAdmissionNamespaces, "") {
		selectedNamespaces = strings.Split(r.Options.ExcludedAdmissionNamespaces, ",")
	}
	if r.Options.ExcludeAdmissionSelfNamespace {
		selectedNamespaces = append(selectedNamespaces, r.Options.CurrentNamespace)
	}

	if len(selectedNamespaces) > 0 {
		tmpWebhookObj.NamespaceSelector = &v1.LabelSelector{}
		tmpWebhookObj.NamespaceSelector.MatchExpressions = []v1.LabelSelectorRequirement{{
			Key:      "kubernetes.io/metadata.name",
			Operator: v1.LabelSelectorOpNotIn,
			Values:   selectedNamespaces,
		}}
	}

	// Ignore admission for resources meeting a special label
	if r.Options.EnableSpecialLabels {
		tmpWebhookObj.ObjectSelector = &v1.LabelSelector{}
		tmpWebhookObj.ObjectSelector.MatchExpressions = []v1.LabelSelectorRequirement{{
			Key:      controller.IgnoreAdmissionLabel,
			Operator: v1.LabelSelectorOpDoesNotExist,
		}}
	}

	sideEffectsClass := admissionregv1.SideEffectClass(admissionregv1.SideEffectClassNone)
	tmpWebhookObj.SideEffects = &sideEffectsClass

	// Replace the webhooks section in the MutatingWebhookConfiguration
	metaWebhookObj.Webhooks = []admissionregv1.MutatingWebhook{tmpWebhookObj}

	return metaWebhookObj, alreadyCreated, nil
}

// webhookRules returns one rule per watched type, taken from the policies in the cluster.
// They are sorted so the same set of policies always produces the same object
func (r *ClusterMutationPolicyReconciler) webhookRules(ctx context.Context) ([]admissionregv1.RuleWithOperations, error) {

	policies := &v1alpha1.ClusterMutationPolicyList{}
	if err := r.List(ctx, policies); err != nil {
		return nil, fmt.Errorf("error listing the ClusterMutationPolicy resources: %w", err)
	}

	patterns := []string{}
	for index := range policies.Items {
		for _, watchedType := range WatchedTypes(&policies.Items[index]) {
			if !slices.Contains(patterns, watchedType) {
				patterns = append(patterns, watchedType)
			}
		}
	}

	slices.Sort(patterns)

	rules := make([]admissionregv1.RuleWithOperations, 0, len(patterns))
	for _, resourcePattern := range patterns {

		resourcePatternParts := strings.Split(resourcePattern, "/")
		if len(resourcePatternParts) != 4 {
			return nil, fmt.Errorf("some key-pattern is invalid on ClusterMutationPolicy registry: %s", resourcePattern)
		}

		rules = append(rules, admissionregv1.RuleWithOperations{
			Rule: admissionregv1.Rule{
				APIGroups:   []string{resourcePatternParts[0]},
				APIVersions: []string{resourcePatternParts[1]},
				Resources:   []string{resourcePatternParts[2]},
				Scope:       &MutatingWebhookConfigurationRuleScopeAll,
			},
			Operations: []admissionregv1.OperationType{admissionregv1.OperationType(resourcePatternParts[3])},
		})
	}

	return rules, nil
}
