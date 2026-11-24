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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ObjectCloneT TODO: Future capability
type ObjectCloneT struct {
}

// ObjectDefinitionT TODO
type ObjectDefinitionT struct {
	Engine   string `json:"engine,omitempty"`
	Template string `json:"template"`
}

// ObjectT TODO
type ObjectT struct {
	Clone      ObjectCloneT      `json:"clone"`
	Definition ObjectDefinitionT `json:"definition"`
}

// ClusterGenerationPolicySpec defines the desired state of ClusterGenerationPolicy
const (
	// DeletionPolicyKeep leaves the generated objects in the cluster when the policy is deleted
	DeletionPolicyKeep string = "Keep"

	// DeletionPolicyDelete removes the objects the policy generated when it is deleted
	DeletionPolicyDelete string = "Delete"
)

type ClusterGenerationPolicySpec struct {
	OverwriteExisting bool `json:"overwriteExisting,omitempty"`

	// DeletionPolicy decides what happens to the generated objects when this policy is deleted.
	// Delete removes everything the policy has written, which includes the objects it adopted
	// when 'overwriteExisting' is enabled
	// +kubebuilder:validation:Enum=Keep;Delete
	// +kubebuilder:default=Keep
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`

	// WatchedResources represents a list of resource-groups that will be watched to be evaluated
	// +listType=map
	// +listMapKey=group
	// +listMapKey=version
	// +listMapKey=resource
	// +listMapKey=name
	// +listMapKey=namespace
	WatchedResources []ResourceGroupT `json:"watchedResources"`

	// Sources represents a list of extra resource-groups to watch and inject in templates
	// +listType=map
	// +listMapKey=group
	// +listMapKey=version
	// +listMapKey=resource
	Sources []SourceGroupT `json:"sources"`

	// Conditions represents a list of conditions that must be passed to meet the policy
	// +listType=map
	// +listMapKey=name
	Conditions []ConditionT `json:"conditions"`

	Object ObjectT `json:"object"`
}

// ClusterGenerationPolicyStatus defines the observed state of ClusterGenerationPolicy
type ClusterGenerationPolicyStatus struct {
	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clustergenerationpolicies,scope=Cluster
// +kubebuilder:subresource:status

// ClusterGenerationPolicy is the Schema for the clustergenerationpolicies API
type ClusterGenerationPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterGenerationPolicySpec   `json:"spec,omitempty"`
	Status ClusterGenerationPolicyStatus `json:"status,omitempty"`
}

func (p *ClusterGenerationPolicy) GetUniqueIdentifier() string {
	return p.Name
}

func (p *ClusterGenerationPolicy) GetSources() []SourceGroupT {
	return p.Spec.Sources
}

// +kubebuilder:object:root=true

// ClusterGenerationPolicyList contains a list of ClusterGenerationPolicy
type ClusterGenerationPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterGenerationPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterGenerationPolicy{}, &ClusterGenerationPolicyList{})
}
