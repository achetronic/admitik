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

package main

import (
	"context"
	"fmt"

	//
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// webhookConfigurationsToClean are the objects the controller creates and Helm does not manage
var webhookConfigurationsToClean = []string{
	"admitik-cluster-validation-policy",
	"admitik-cluster-mutation-policy",
}

// cleanupWebhookConfigurations removes the webhook configurations, so an uninstall does not leave
// the cluster calling a Service that no longer exists
func cleanupWebhookConfigurations(ctx context.Context) error {

	client, err := kubernetes.NewForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		return fmt.Errorf("error building the kubernetes client: %w", err)
	}

	options := metav1.DeleteOptions{}

	for _, name := range webhookConfigurationsToClean {

		err = client.AdmissionregistrationV1().ValidatingWebhookConfigurations().
			Delete(ctx, name, options)
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("error deleting the validating webhook configuration '%s': %w", name, err)
		}

		err = client.AdmissionregistrationV1().MutatingWebhookConfigurations().
			Delete(ctx, name, options)
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("error deleting the mutating webhook configuration '%s': %w", name, err)
		}
	}

	return nil
}
