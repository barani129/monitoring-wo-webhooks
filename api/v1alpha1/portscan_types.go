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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	PortEventSource                 = "PortScan"
	PortEventReasonIssuerReconciler = "PortScanReconciler"
)

// PortScanSpec defines the desired state of PortScan
type PortScanSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Target is the Openshift/kubernetes cluster hostname URL or any IP/FQDN with port (example: api.test.com:443 ) (If FQDN is used, it should be resolvable via DNS query)
	Target []string `json:"target"`

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

	// frequency of the check. If not set, it defaults to 2 mins.
	// +optional
	CheckInterval *int64 `json:"checkInterval,omitempty"`
}

// PortScanStatus defines the observed state of PortScan
type PortScanStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// list of status conditions to indicate the status of managed cluster
	// known conditions are 'Ready'.
	// +optional
	Conditions []PortScanCondition `json:"conditions,omitempty"`

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

	// affected targets
	// +optional
	AffectedTargets []string `json:"affectedTargets,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// PortScan is the Schema for the PortScans API
// +kubebuilder:printcolumn:name="CreatedAt",type="string",JSONPath=".metadata.creationTimestamp",description="object creation timestamp(in cluster's timezone)"
// +kubebuilder:printcolumn:name="Reachable",type="string",JSONPath=".status.conditions[].status",description="whether cluster is reachable on the give IP and port"
// +kubebuilder:printcolumn:name="LastSuccessfulPollTime",type="string",JSONPath=".status.lastPollTime",description="last poll timestamp(in cluster's timezone)"
// +kubebuilder:printcolumn:name="ExternalNotified",type="string",JSONPath=".status.externalNotified",description="indicates if the external system is notified"
// +kubebuilder:printcolumn:name="IncidentID",type="string",JSONPath=".status.incidentID",description="incident ID from service now"
type PortScan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PortScanSpec   `json:"spec,omitempty"`
	Status PortScanStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// PortScanList contains a list of PortScan
type PortScanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PortScan `json:"items"`
}

type PortScanCondition struct {
	// Type of the condition, known values are 'Ready'.
	Type PortScanConditionType `json:"type"`

	// Status of the condition, one of ('True', 'False', 'Unknown')
	Status ConditionStatus `json:"status"`

	// LastTransitionTime is the timestamp of the last update to the status
	// +optional
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`

	// Reason is the machine readable explanation for object's condition
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message is the human readable explanation for object's condition
	Message string `json:"message"`
}

// ManagedConditionType represents a managed cluster condition value.
type PortScanConditionType string

const (
	// PortScanConditionReady represents the fact that a given managed cluster condition
	// is in reachable from the ACM/source cluster.
	// If the `status` of this condition is `False`, managed cluster is unreachable
	PortScanConditionReady PortScanConditionType = "Ready"
)

func init() {
	SchemeBuilder.Register(&PortScan{}, &PortScanList{})
}
