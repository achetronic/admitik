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

// AuditResult is the result of one policy over one resource type
type AuditResult struct {
	// Policy is the name of the policy that was evaluated
	Policy string `json:"policy"`

	// Kind is the kind of the policy
	Kind string `json:"kind"`

	// Resource identifies the resource type, as {group}/{version}/{resource}
	Resource string `json:"resource"`

	// Scanned is the number of objects the policy was evaluated against
	Scanned int `json:"scanned"`

	// Compliant is the number of objects that met the conditions
	Compliant int `json:"compliant"`

	// NonCompliant is the number of objects that did not meet the conditions
	NonCompliant int `json:"nonCompliant"`

	// Errors is the number of objects that could not be evaluated
	Errors int `json:"errors"`

	// Samples holds a few non compliant objects, as namespace/name
	Samples []string `json:"samples,omitempty"`
}

// AuditSummary aggregates the results of a scan
type AuditSummary struct {
	// Policies is the number of policies that were evaluated
	Policies int `json:"policies"`

	// Scanned is the number of objects that were looked at
	Scanned int `json:"scanned"`

	// Compliant is the number of objects that met the conditions of every policy
	Compliant int `json:"compliant"`

	// NonCompliant is the number of objects that did not meet the conditions of some policy
	NonCompliant int `json:"nonCompliant"`

	// Errors is the number of objects that could not be evaluated
	Errors int `json:"errors"`
}

// ClusterAuditReportStatus is the result of the last compliance scan
type ClusterAuditReportStatus struct {
	// ScanTime is when the scan started
	ScanTime metav1.Time `json:"scanTime,omitempty"`

	// Duration is how long the scan took, as a human readable value
	Duration string `json:"duration,omitempty"`

	// Summary aggregates the results
	Summary AuditSummary `json:"summary,omitempty"`

	// Results holds one entry per policy and resource type
	Results []AuditResult `json:"results,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clusterauditreports,scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Scan",type=date,JSONPath=`.status.scanTime`
// +kubebuilder:printcolumn:name="Scanned",type=integer,JSONPath=`.status.summary.scanned`
// +kubebuilder:printcolumn:name="NonCompliant",type=integer,JSONPath=`.status.summary.nonCompliant`

// ClusterAuditReport is the result of evaluating the validation policies over the objects that
// already exist in the cluster. Objects are evaluated with the CREATE operation, as a recreation
type ClusterAuditReport struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Status ClusterAuditReportStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterAuditReportList contains a list of ClusterAuditReport
type ClusterAuditReportList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterAuditReport `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterAuditReport{}, &ClusterAuditReportList{})
}
