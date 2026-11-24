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
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/freepik-company/admitik/api/v1alpha1"
	"github.com/freepik-company/admitik/internal/controller"
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
)

// ReconcileClusterMutationPolicy keeps internal ClusterMutationPolicy resources' registry up-to-date
func (r *ClusterMutationPolicyReconciler) ReconcileClusterMutationPolicy(
	ctx context.Context, eventType watch.EventType, resourceManifest *v1alpha1.ClusterMutationPolicy) (err error) {

	logger := log.FromContext(ctx)

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

// SyncWebhookConfiguration writes the webhook configuration the cluster should have, built from
// the policies that exist right now. It must only be called by the leader
func (r *ClusterMutationPolicyReconciler) SyncWebhookConfiguration(ctx context.Context) error {

	configuration := &admissionregv1.MutatingWebhookConfiguration{}
	configuration.Name = MutatingWebhookConfigurationName

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, configuration, func() error {
		patterns, err := r.watchedTypes(ctx)
		if err != nil {
			return err
		}

		rules, err := controller.WebhookRules(patterns)
		if err != nil {
			return err
		}

		configuration.Webhooks = []admissionregv1.MutatingWebhook{
			controller.NewMutatingWebhook(controller.WebhookOptions{
				Name:                 "mutate.admitik.svc",
				Rules:                rules,
				ClientConfig:         r.Options.WebhookClientConfig,
				TimeoutSeconds:       int32(r.Options.WebhookTimeout),
				CurrentNamespace:     r.Options.CurrentNamespace,
				ExcludeSelfNamespace: r.Options.ExcludeAdmissionSelfNamespace,
				ExcludedNamespaces:   r.Options.ExcludedAdmissionNamespaces,
				IgnoreLabeledObjects: r.Options.EnableSpecialLabels,
			}),
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error syncing the MutatingWebhookConfiguration '%s': %w", MutatingWebhookConfigurationName, err)
	}

	return nil
}

// watchedTypes returns the collection keys of every policy in the cluster
func (r *ClusterMutationPolicyReconciler) watchedTypes(ctx context.Context) ([]string, error) {

	policies := &v1alpha1.ClusterMutationPolicyList{}
	if err := r.List(ctx, policies); err != nil {
		return nil, fmt.Errorf("error listing the ClusterMutationPolicy resources: %w", err)
	}

	patterns := []string{}
	for index := range policies.Items {
		patterns = append(patterns, WatchedTypes(&policies.Items[index])...)
	}

	return patterns, nil
}
