/*
Copyright 2025.

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

type FrontendSpec struct {
	Image    string `json:"image"`
	Replicas int32  `json:"replicas,omitempty"`
}

type BackendSpec struct {
	Image    string `json:"image"`
	Replicas int32  `json:"replicas,omitempty"`

	LogLevel         string `json:"log_level,omitempty"`
	EnableConsoleLog bool   `json:"enable_console_log,omitempty"`
	GinMode          string `json:"gin_mode,omitempty"`
}

type DatabaseSpec struct {
	PGClusterVersion int32 `json:"pg_cluster_version"`

	Instances   int32  `json:"instances"`
	StorageSize string `json:"storage_size"`

	PostgresVersion string      `json:"postgres_version"`
	Backup          *BackupSpec `json:"backup,omitempty"`
}

type BackupSpec struct {
	GCSBucket                   string             `json:"gcs_bucket"`
	GCPCredentials              GCPCredentialsSpec `json:"gcpCredentials"`
	Schedule                    string             `json:"schedule,omitempty"`
	BootStrapFromPreviousBackup bool               `json:"bootstrap_from_previous_backup,omitempty"`
}

type GCPCredentialsSpec struct {
	SecretName string `json:"secret_name"`
}

// MinimalDoSpec defines the desired state of MinimalDo.
type MinimalDoSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of MinimalDo. Edit minimaldo_types.go to remove/update
	Frontend FrontendSpec `json:"frontend"`
	Backend  BackendSpec  `json:"backend"`
	Database DatabaseSpec `json:"database"`

	ApplicaionDomain    string `json:"application_domain"`
	ImagePullSecretName string `json:"image_pull_secret_name"`
}

// MinimalDoStatus defines the observed state of MinimalDo.
type MinimalDoStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// MinimalDo is the Schema for the minimaldoes API.
type MinimalDo struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MinimalDoSpec   `json:"spec,omitempty"`
	Status MinimalDoStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// MinimalDoList contains a list of MinimalDo.
type MinimalDoList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MinimalDo `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MinimalDo{}, &MinimalDoList{})
}
