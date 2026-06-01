package usecase

import (
	"context"
	"maps"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (u *HermesAgentUseCase) reconcileService(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	nsName := types.NamespacedName{Name: ha.Name, Namespace: ha.Namespace}

	existing, err := u.kube.GetService(ctx, GetServiceParam{NamespacedName: nsName})
	if err != nil {
		return err
	}

	desired := buildService(ha)
	if existing != nil {
		desired.ResourceVersion = existing.ResourceVersion
		// ClusterIP is immutable; carry it over from the existing Service.
		desired.Spec.ClusterIP = existing.Spec.ClusterIP
		err := u.kube.UpdateServiceOwnedByHermesAgent(ctx, UpdateServiceParam{HermesAgent: ha, Service: desired})
		u.tel.IncServiceOperation(ctx, IncServiceOperationParam{NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}, Operation: OperationUpdate, Result: resultOf(err)})
		return err
	}

	err = u.kube.CreateServiceOwnedByHermesAgent(ctx, CreateServiceOfHermesAgentParam{HermesAgent: ha, Service: desired})
	u.tel.IncServiceOperation(ctx, IncServiceOperationParam{NamespacedName: types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}, Operation: OperationCreate, Result: resultOf(err)})
	return err
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
			Ports:    buildServicePorts(svc),
		},
	}
}

func buildServicePorts(svc *agentsv1alpha1.Service) []corev1.ServicePort {
	if svc == nil || len(svc.Ports) == 0 {
		return []corev1.ServicePort{
			{
				Name:       hermesGatewayPortName,
				Port:       hermesGatewayPort,
				TargetPort: intstr.FromInt32(hermesGatewayPort),
				Protocol:   corev1.ProtocolTCP,
			},
		}
	}

	ports := make([]corev1.ServicePort, 0, len(svc.Ports))
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
