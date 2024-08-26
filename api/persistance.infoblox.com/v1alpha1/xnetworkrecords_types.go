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
	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// XNetworkRecordsSpec defines the desired state of XNetworkRecords
type XNetworkRecordsSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	xpv1.ResourceSpec `json:",inline"`

	// Parameters are used to configured the XR, all are required
	Parameters XNetworkRecordsParameters `json:"parameters"`
}

type XNetworkRecordsParameters struct {
	// Network is the VPC network to provision IP addresses for
	// PSC
	// ie. projects/gcp-eng-ddiaas-dev/global/networks/alloydb-psc-network
	Network string `json:"network"`

	// PSCDNSName is the DNS name of the PSC
	// ie. 30f6af49-74c7-4058-9b00-8a29cff777c9.3f031303-8e9c-4941-8b77-1aafad235014.us-east1.alloydb-psc.goog.
	PSCDNSName string `json:"pscDNSName"`

	// Region is the region of the PSC
	// ie. us-east1
	Region string `json:"region"`

	// ServiceAttachmentLink is the URL of the service attachment
	// ie. https://www.googleapis.com/compute/v1/projects/gcp-eng-ddiaas-dev/regions/us-east1/serviceAttachments/alloydb-psc-network-sx9s5
	ServiceAttachmentLink string `json:"serviceAttachmentLink"`

	// Subnetwork is the subnet to provision IP addresses for PSC
	// ie. projects/gcp-eng-ddiaas-dev/regions/us-east1/subnetworks/private-service-connect
	Subnetwork string `json:"subnetwork"`
}

// XNetworkRecordsStatus defines the observed state of XNetworkRecords
type XNetworkRecordsStatus struct {
	xpv1.ResourceStatus `json:",inline"`
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Address is the local IP address in the VPC ie. 100.127.252.5
	Address string `json:"address,omitempty"`

	// AddressID is the fully qualified resource ID for the address ie. projects/gcp-eng-ddiaas-dev/regions/us-east1/addresses/alloydb-psc-network-h9h4h
	// TODO: if the managed address url can be predetermined, we do not need this field
	AddressID string `json:"addressID,omitempty"`

	// ManagedZoneRef is the name of the Managed Zone CR
	// ie. alloydb-psc-network-sx9s5
	// TODO: if managed zone resource name can be predetermined, this field is not needed
	ManagedZoneRef string `json:"managedZoneRef,omitempty"`
}

// Generate RBAC for these types

// +kubebuilder:rbac:groups=persistance.infoblox.com,resources=xnetworkrecords,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=persistance.infoblox.com,resources=xnetworkrecords/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=persistance.infoblox.com,resources=xnetworkrecords/finalizers,verbs=update

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=xnr
// +kubebuilder:subresource:status
// +kubebuilder:noskip

// XNetworkRecords is the Schema for the xnetworkrecords API
type XNetworkRecords struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   XNetworkRecordsSpec   `json:"spec,omitempty"`
	Status XNetworkRecordsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// Disable code generates until crossplane-tools matures
// https://github.com/crossplane/crossplane-tools
// +kubebuilder:skip

// XNetworkRecordsList contains a list of XNetworkRecords
type XNetworkRecordsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []XNetworkRecords `json:"items"`
}

func init() {
	SchemeBuilder.Register(&XNetworkRecords{}, &XNetworkRecordsList{})
}
