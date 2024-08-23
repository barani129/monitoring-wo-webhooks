/*
Copyright 2024 baranitharan.chittharanjan@spark.co.nz.

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

const (
	VmScanEventSource                 = "VmScan"
	VmScanEventReasonIssuerReconciler = "VmScanScanReconciler"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VmScanSpec defines the desired state of VmScan
type VmScanSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// ImVmant: Run "make" to regenerate code after modifying this file

	// Set suspend to true to disable monitoring the custom resource
	// +optional
	Suspend *bool `json:"suspend"`

	// target namespace to check for vmi
	TargetNamespace []string `json:"targetNamespace"`

	// check interval in integer
	// +optional
	CheckInterval *int64 `json:"checkInterval,omitempty"`

	// Suspends email alerts if set to true, target users will not be notified
	// +optional
	SuspendEmailAlert *bool `json:"suspendEmail,omitempty"`

	// Target user's email for cluster status notification
	// +optional
	Email string `json:"email,omitempty"`

	// Relay host for sending the email
	// +optional
	RelayHost string `json:"relayHost,omitempty"`

	// To notify the external alerting system
	// +optional
	NotifyExtenal *bool `json:"notifyExternal,omitempty"`

	// URL of the external alert system. Example: http://notify.example.com/ (both http/https supported with basic authentication)
	// +optional
	ExternalURL string `json:"externalURL,omitempty"`

	// Data to be sent to the external system in the form of config map
	// +optional
	ExternalData string `json:"externalData,omitempty"`

	// Secret which has the username and password to post the alert notification to the external system using Authorization header
	// +optional
	ExternalSecret string `json:"externalSecret,omitempty"`
}

// VmScanStatus defines the observed state of VmScan
type VmScanStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// ImVmant: Run "make" to regenerate code after modifying this file
	// list of status conditions to indicate the status of managed cluster
	// known conditions are 'Ready'.
	// +optional
	Conditions []VmScanCondition `json:"conditions,omitempty"`

	// last successful timestamp of retrieved cluster status
	// +optional
	LastPollTime *metav1.Time `json:"lastPollTime,omitempty"`

	// Indicates if external alerting system is notified
	// +optional
	ExternalNotified bool `json:"externalNotified,omitempty"`

	// Indicates the timestamp when external alerting system is notified
	// +optional
	ExternalNotifiedTime *metav1.Time `json:"externalNotifiedTime,omitempty"`

	// Incident ID from the rem. Spark specific
	// +optional
	IncidentID []string `json:"incidentID,omitempty"`

	// list of affected vmi and node
	// +optional
	AffectedTargets []string `json:"affectedTargets,omitempty"`

	// list of ongoing migration/failed migrations
	// +optional
	Migrations []string `json:"migrations,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// VmScan is the Schema for the vmscans API
// +kubebuilder:printcolumn:name="CreatedAt",type="string",JSONPath=".metadata.creationTimestamp",description="object creation timestamp(in cluster's timezone)"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[].status",description="whether cluster is reachable on the give IP and port"
// +kubebuilder:printcolumn:name="LastNonViolation",type="string",JSONPath=".status.lastPollTime",description="last poll timestamp(in cluster's timezone)"
// +kubebuilder:printcolumn:name="ExternalNotified",type="string",JSONPath=".status.externalNotified",description="indicates if the external system is notified"
// +kubebuilder:printcolumn:name="IncidentID",type="string",JSONPath=".status.incidentID",description="incident ID from service now"
// +kubebuilder:printcolumn:name="Migrations",type="string",JSONPath=".status.migrations",description="list of VMs with namespace that are either migrating/failed to migrate"
type VmScan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VmScanSpec   `json:"spec,omitempty"`
	Status VmScanStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VmScanList contains a list of VmScan
type VmScanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VmScan `json:"items"`
}

type VmScanCondition struct {
	// Type of the condition, known values are 'NonViolation'.
	Type VmScanConditionType `json:"type"`

	// Status of the condition, one of ('Violated', 'NonViolated', 'Unknown')
	Status VmConditionStatus `json:"status"`

	// LastTransitionTime is the timestamp of the last update to the status
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

	// Reason is the machine readable explanation for object's condition
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message is the human readable explanation for object's condition
	Message string `json:"message"`
}

// ConditionStatus represents a condition's status.
// +kubebuilder:validation:Enum=Violated;NonViolated;Unknown
type VmConditionStatus string

// These are valid condition statuses. "ConditionTrue" means a resource is in
// the condition; "ConditionFalse" means a resource is not in the condition;
// "ConditionUnknown" means kubernetes can't decide if a resource is in the
// condition or not. In the future, we could add other intermediate
// conditions, e.g. ConditionDegraded.
const (
	// ConditionTrue represents the fact that a given condition is true
	ConditionViolated VmConditionStatus = "Violated"

	// ConditionNonViolated represents the fact that a given condition is non violated
	ConditionNonViolated VmConditionStatus = "NonViolated"

	// ConditionUnknown represents the fact that a given condition is unknown
	ConditionStatusUnknown VmConditionStatus = "Unknown"
)

// ManagedConditionType represents a managed cluster condition value.
type VmScanConditionType string

const (
	// VmScanConditionNonViolation represents the fact that a given managed cluster condition
	// is in reachable from the ACM/source cluster.
	// If the `status` of this condition is `False`, managed cluster is unreachable
	VmScanConditionNonViolation VmScanConditionType = "PlacementStatus"
)

func init() {
	SchemeBuilder.Register(&VmScan{}, &VmScanList{})
}
