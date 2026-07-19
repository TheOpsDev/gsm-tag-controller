/*
Copyright 2026.

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
	"k8s.io/apimachinery/pkg/runtime"
)

// DeletionPolicy determines whether GCP tag bindings are removed when the
// GSMSecretTag is deleted.
// +kubebuilder:validation:Enum=Retain;Delete
type DeletionPolicy string

const (
	// DeletionPolicyRetain leaves GCP tag bindings in place on deletion.
	DeletionPolicyRetain DeletionPolicy = "Retain"
	// DeletionPolicyDelete removes GCP tag bindings before deletion.
	DeletionPolicyDelete DeletionPolicy = "Delete"
)

// GSMSecretTagSpec defines the desired state of GSMSecretTag
type GSMSecretTagSpec struct {
	// secretTarget is the name of the GSM secret to tag, taken from the
	// owning PushSecret's RemoteKey.
	// +required
	// +kubebuilder:validation:MinLength=1
	SecretTarget string `json:"secretTarget"`

	// tags holds the Resource Manager tag short names to bind to the secret,
	// as key/value short-name pairs. Keys and values must already exist under
	// the configured GCP organization; the controller only binds them.
	// +required
	// +kubebuilder:validation:MinProperties=1
	Tags map[string]string `json:"tags"`

	// deletionPolicy controls whether GCP tag bindings are removed when this
	// resource is deleted.
	// +optional
	// +kubebuilder:default=Retain
	DeletionPolicy DeletionPolicy `json:"deletionPolicy,omitempty"`
}

// GSMSecretTagStatus defines the observed state of GSMSecretTag.
type GSMSecretTagStatus struct {
	// conditions represent the current state of the GSMSecretTag resource,
	// including errors such as tag keys or values missing in GCP.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedTags reflects the tag bindings currently applied to the GSM
	// secret in GCP. Drift between this and spec.tags triggers a full sync.
	// +optional
	ObservedTags map[string]string `json:"observedTags,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.secretTarget`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// GSMSecretTag is the Schema for the gsmsecrettags API
type GSMSecretTag struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of GSMSecretTag
	// +required
	Spec GSMSecretTagSpec `json:"spec"`

	// status defines the observed state of GSMSecretTag
	// +optional
	Status GSMSecretTagStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// GSMSecretTagList contains a list of GSMSecretTag
type GSMSecretTagList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []GSMSecretTag `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &GSMSecretTag{}, &GSMSecretTagList{})
		return nil
	})
}
