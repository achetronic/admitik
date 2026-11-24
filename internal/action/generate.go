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

package action

import (
	"context"
	"fmt"

	//
	"github.com/go-logr/logr"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/log"

	//
	"github.com/admitikcore/admitik/api/v1alpha1"
	"github.com/admitikcore/admitik/internal/common"
	"github.com/admitikcore/admitik/internal/evaluator"
	"github.com/admitikcore/admitik/internal/kubeapply"
	"github.com/admitikcore/admitik/internal/policystore"
	"github.com/admitikcore/admitik/internal/template"
)

const (
	// GenerationPolicyKind is written on the generated objects as their origin
	GenerationPolicyKind = "ClusterGenerationPolicy"

	//
	templateFailureMessage = "Generation template failed. More info in controller logs."
	objectFailureMessage   = "Invalid object after template. More info in controller logs."
	writeFailureMessage    = "Object write after template failed. More info in controller logs."
	existingObjectMessage  = "The object already exists and 'overwriteExisting' is disabled"
)

// Generate writes the object a ClusterGenerationPolicy renders for an event
type Generate struct {
	dependencies GenerateDependencies
}

type GenerateDependencies struct {
	Policies  *policystore.PolicyStore[*v1alpha1.ClusterGenerationPolicy]
	Evaluator *evaluator.Evaluator
	Engines   *template.Registry
	Writer    *kubeapply.Writer
}

func NewGenerate(dependencies GenerateDependencies) *Generate {
	return &Generate{dependencies: dependencies}
}

// Process renders the object of every policy that watches the event and writes it
func (g *Generate) Process(
	ctx context.Context, resourceType string, eventType watch.EventType, object ...map[string]any) {

	logger := log.FromContext(ctx).WithValues("action", "generate", "watched", resourceType)

	policies := g.dependencies.Policies.Store.GetResources(resourceType)
	for _, policy := range policies {

		policyLogger := logger.WithValues(GenerationPolicyKind, policy.Name)

		userMessage, err := g.generate(ctx, policyLogger, policy, eventType, object)
		if err != nil {
			policyLogger.Info(err.Error())
		}

		// The user is told why the object is not there, in the namespace of the object that was watched
		if userMessage == "" {
			continue
		}

		eventErr := common.CreateKubeEvent(ctx, common.EventNamespace(object[0]), "resources-controller",
			object[0], *policy, "GenerationAborted", userMessage)
		if eventErr != nil {
			policyLogger.Info(fmt.Sprintf("failed creating Kubernetes event: %s", eventErr.Error()))
		}
	}
}

// generate renders the object of one policy and writes it.
// It returns the message shown to the user when the object could not be written, if any
func (g *Generate) generate(
	ctx context.Context,
	logger logr.Logger,
	policy *v1alpha1.ClusterGenerationPolicy,
	eventType watch.EventType,
	object []map[string]any,
) (string, error) {

	data := template.NewInjectedData()
	data.Operation = common.GetNormalizedOperation(eventType)
	data.Object = object[0]
	if data.Operation == common.NormalizedOperationUpdate && len(object) > 1 {
		data.OldObject = object[1]
	}

	prepared := g.dependencies.Evaluator.Prepare(evaluator.Request{
		Policy:     policy,
		Conditions: policy.Spec.Conditions,
	}, data)
	if prepared.Err != nil {
		logger.Info("policy could not be prepared", "reason", prepared.Reason, "error", prepared.Err.Error())
	}

	if !prepared.Passed {
		return "", nil
	}

	rendered, err := g.dependencies.Engines.Evaluate(
		policy.Spec.Object.Definition.Engine, policy.Spec.Object.Definition.Template, data)
	if err != nil {
		return templateFailureMessage, fmt.Errorf("error rendering the object of '%s': %w", policy.Name, err)
	}

	definition := map[string]any{}
	if err = yaml.Unmarshal([]byte(rendered), &definition); err != nil {
		return objectFailureMessage, fmt.Errorf("error decoding the rendered object of '%s': %w", policy.Name, err)
	}

	result, err := g.dependencies.Writer.Apply(ctx, kubeapply.Options{
		PolicyName: policy.Name,
		PolicyKind: GenerationPolicyKind,
		Overwrite:  policy.Spec.OverwriteExisting,
	}, definition)
	if err != nil {
		return writeFailureMessage, fmt.Errorf("error writing the object of '%s': %w", policy.Name, err)
	}

	logger = logger.WithValues("resource", result.Resource, "name", result.Name, "namespace", result.Namespace)

	if result.Existing {
		return existingObjectMessage, nil
	}

	logger.Info("generated object written", "created", result.Created)
	return "", nil
}
