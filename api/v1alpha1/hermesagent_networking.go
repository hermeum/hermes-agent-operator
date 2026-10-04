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
	corev1 "k8s.io/api/core/v1"
)

// Networking defines network-related configuration.
type Networking struct {
	// service configures the Kubernetes Service.
	// +optional
	Service Service `json:"service,omitempty"`

	// ingress configures the Kubernetes Ingress.
	// +optional
	Ingress Ingress `json:"ingress,omitempty"`
}

// Service defines the Service configuration.
type Service struct {
	// type is the Kubernetes Service type.
	// +kubebuilder:validation:Enum=ClusterIP;LoadBalancer;NodePort
	// +kubebuilder:default="ClusterIP"
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`

	// annotations to add to the Service.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// ports defines additional ports exposed on the Service.
	// The API server port (config.apiServer.port, default 8642) is always
	// exposed and should not be repeated here.
	// +kubebuilder:validation:MaxItems=20
	// +optional
	Ports []ServicePort `json:"ports,omitempty"`
}

// ServicePort defines a port exposed by the Service.
type ServicePort struct {
	// name is the name of the port.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// port is the port number exposed on the Service.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	// targetPort is the port on the container to route to (defaults to port).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	TargetPort *int32 `json:"targetPort,omitempty"`

	// protocol is the protocol for the port.
	// +kubebuilder:validation:Enum=TCP;UDP;SCTP
	// +kubebuilder:default="TCP"
	// +optional
	Protocol corev1.Protocol `json:"protocol,omitempty"`
}

// Ingress defines the Ingress configuration.
type Ingress struct {
	// enabled enables Ingress creation.
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// className is the name of the IngressClass to use.
	// +optional
	ClassName *string `json:"className,omitempty"`

	// annotations to add to the Ingress.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// hosts is a list of hosts to route traffic for.
	// +optional
	Hosts []IngressHost `json:"hosts,omitempty"`

	// tls configuration.
	// +optional
	TLS []IngressTLS `json:"tls,omitempty"`
}

// IngressHost defines a host for the Ingress.
type IngressHost struct {
	// host is the fully qualified domain name.
	Host string `json:"host"`

	// paths is a list of paths to route.
	// +kubebuilder:validation:MinItems=1
	Paths []IngressPath `json:"paths"`
}

// IngressPath defines a path for the Ingress.
type IngressPath struct {
	// path is the path to route.
	// +kubebuilder:default="/"
	// +optional
	Path string `json:"path,omitempty"`

	// pathType determines how the path should be matched.
	// +kubebuilder:validation:Enum=Prefix;Exact;ImplementationSpecific
	// +kubebuilder:default="Prefix"
	// +optional
	PathType string `json:"pathType,omitempty"`

	// port is the backend service port number to route traffic to.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}

// IngressTLS defines TLS configuration for the Ingress.
type IngressTLS struct {
	// hosts are a list of hosts included in the TLS certificate.
	Hosts []string `json:"hosts,omitempty"`

	// secretName is the name of the secret containing the TLS certificate.
	SecretName string `json:"secretName,omitempty"`
}

func (n *Networking) GetService() *Service {
	if n == nil {
		return nil
	}
	return &n.Service
}

func (n *Networking) GetIngress() *Ingress {
	if n == nil {
		return nil
	}
	return &n.Ingress
}

func (s *Service) GetType() corev1.ServiceType {
	if s == nil || s.Type == "" {
		return corev1.ServiceTypeClusterIP
	}
	return s.Type
}

func (i *Ingress) IsEnabled() bool {
	return i != nil && i.Enabled
}
