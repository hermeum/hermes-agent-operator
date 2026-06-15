package usecase

import (
	"context"
	"maps"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (u *HermesAgentUseCase) reconcileService(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}

	existing, err := u.kube.GetService(ctx, GetServiceParam{NamespacedName: nsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	desired := buildService(ha)
	if len(desired.Spec.Ports) == 0 {
		if existing != nil {
			if err := u.kube.DeleteService(ctx, DeleteServiceParam{NamespacedName: nsName}); err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "Service deleted (no ports configured)", "namespacedName", nsName)
		}
		ha.Status.ManagedResources.Service = ""
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{}, nil
	}

	if existing != nil {
		// ClusterIP is immutable; carry it over from the existing Service.
		desired.Spec.ClusterIP = existing.Spec.ClusterIP
		if serviceEqual(desired, existing) {
			ha.Status.ManagedResources.Service = ha.Name
			return ctrl.Result{}, nil
		}
		desired.ResourceVersion = existing.ResourceVersion
		err := u.kube.UpdateServiceOwnedByHermesAgent(ctx, UpdateServiceParam{HermesAgent: ha, Service: desired})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Service updated", "namespacedName", nsName)
		ha.Status.ManagedResources.Service = ha.Name
		return ctrl.Result{}, nil
	}

	err = u.kube.CreateServiceOwnedByHermesAgent(ctx, CreateServiceOfHermesAgentParam{HermesAgent: ha, Service: desired})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "Service created", "namespacedName", nsName)
	ha.Status.ManagedResources.Service = ha.Name
	return ctrl.Result{}, nil
}

func serviceEqual(a, b *corev1.Service) bool {
	return equality.Semantic.DeepEqual(a.Spec.Ports, b.Spec.Ports) &&
		a.Spec.Type == b.Spec.Type &&
		maps.Equal(a.Annotations, b.Annotations)
}

func buildService(ha *agentsv1alpha1.HermesAgent) *corev1.Service {
	svc := ha.GetNetworking().GetService()

	var annotations map[string]string
	if svc != nil && len(svc.Annotations) > 0 {
		annotations = maps.Clone(svc.Annotations)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ha.Name,
			Namespace:   ha.Namespace,
			Labels:      resourceLabels(ha),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     svc.GetType(),
			Selector: selectorLabels(ha),
			Ports:    buildServicePorts(ha, svc),
		},
	}
}

func buildServicePorts(ha *agentsv1alpha1.HermesAgent, svc *agentsv1alpha1.Service) []corev1.ServicePort {
	apiServer := ha.GetHermes().GetAPIServer()
	webhook := ha.GetHermes().GetWebhook()
	var ports []corev1.ServicePort
	if apiServer.IsEnabled() {
		ports = append(ports, corev1.ServicePort{
			Name:       apiServer.GetPortName(),
			Port:       apiServer.GetPort(),
			TargetPort: intstr.FromInt32(apiServer.GetPort()),
			Protocol:   corev1.ProtocolTCP,
		})
	}
	if webhook.IsEnabled() {
		ports = append(ports, corev1.ServicePort{
			Name:       webhook.GetPortName(),
			Port:       webhook.GetPort(),
			TargetPort: intstr.FromInt32(webhook.GetPort()),
			Protocol:   corev1.ProtocolTCP,
		})
	}
	if svc == nil {
		return ports
	}

	for _, p := range svc.Ports {
		target := p.Port
		if p.TargetPort != nil {
			target = *p.TargetPort
		}
		protocol := p.Protocol
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}
		ports = append(ports, corev1.ServicePort{
			Name:       p.Name,
			Port:       p.Port,
			TargetPort: intstr.FromInt32(target),
			Protocol:   protocol,
		})
	}
	return ports
}
