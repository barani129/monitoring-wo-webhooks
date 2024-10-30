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
	MetallbEventSource                 = "MetallbScan"
	MetallbEventReasonIssuerReconciler = "MetallbScanReconciler"
)

// MetallbScanSpec defines the desired state of MetallbScan
type MetallbScanSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Set suspend to true to disable monitoring the custom resource
	// +optional
	Suspend *bool `json:"suspend"`

	// Label for selecting workers
	// +optional
	WorkerLabel *map[string]string `json:"workerLabel"`

	// Label for selecting workers
	// +optional
	SpeakerPodLabel *map[string]string `json:"speakerPodLabel"`

	// Suspends email alerts if set to true, target users will not be notified
	// +optional
	SuspendEmailAlert *bool `json:"suspendEmail,omitempty"`

	// Target user's email for cluster status notification
	// +optional
	Email string `json:"email,omitempty"`

	// Target cluster name for using the cluster name in notifications
	// +optional
	Cluster *string `json:"cluster,omitempty"`

	// Target metallb namespace
	// +optional
	MetallbNamespace *string `json:"metalLbNamespace,omitempty"`

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

// MetallbScanStatus defines the observed state of MetallbScan
type MetallbScanStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// list of status conditions to indicate the status of managed cluster
	// known conditions are 'Ready'.
	// +optional
	Conditions []MetallbScanCondition `json:"conditions,omitempty"`

	// last successful timestamp of retrieved cluster status
	// +optional
	LastRunTime *metav1.Time `json:"lastRunTime,omitempty"`

	// Incident ID from the rem. Spark specific
	// +optional
	IncidentID []string `json:"incidentID,omitempty"`

	// affected targets
	// +optional
	FailedChecks []string `json:"failedChecks,omitempty"`

	// last successful timestamp of metallbscan, indicates if the metallb checks are completely healthy
	// +optional
	LastSuccessfulRunTime *metav1.Time `json:"lastSuccessfulRunTime,omitempty"`

	// Indicates if all load balancer type services running fine/IPs are advertised by metallb speaker pods in the target cluster
	// +optional
	Healthy bool `json:"healthy,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// MetallbScan is the Schema for the metallbscans API
// +kubebuilder:printcolumn:name="CreatedAt",type="string",JSONPath=".metadata.creationTimestamp",description="object creation timestamp(in cluster's timezone)"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[].status",description="if set to true, there is no container with non-zero terminate state"
// +kubebuilder:printcolumn:name="LastRunTime",type="string",JSONPath=".status.lastRunTime",description="last healthcheck run timestamp(in cluster's timezone)"
// +kubebuilder:printcolumn:name="LastSuccessfulRunTime",type="string",JSONPath=".status.lastSuccessfulRunTime",description="last successful run timestamp(in cluster's timezone) where cluster is error free "
// +kubebuilder:printcolumn:name="Healthy",type="string",JSONPath=".status.healthy",description="last successful run (where there is no failed containers) timestamp(in cluster's timezone)"
type MetallbScan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MetallbScanSpec   `json:"spec,omitempty"`
	Status MetallbScanStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MetallbScanList contains a list of MetallbScan
type MetallbScanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MetallbScan `json:"items"`
}

type MetallbScanCondition struct {
	// Type of the condition, known values are 'Ready'.
	Type MetallbScanConditionType `json:"type"`

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
type MetallbScanConditionType string

const (
	// MetallbScanConditionReady represents the fact that a given managed cluster condition
	// is in reachable from the ACM/source cluster.
	// If the `status` of this condition is `False`, managed cluster is unreachable
	MetallbScanConditionReady MetallbScanConditionType = "Ready"
)

func init() {
	SchemeBuilder.Register(&MetallbScan{}, &MetallbScanList{})
}
