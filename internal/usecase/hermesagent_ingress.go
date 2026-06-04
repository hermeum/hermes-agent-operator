package usecase

import (
	"context"
	"maps"
	"time"

	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (u *HermesAgentUseCase) reconcileIngress(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}

	existing, err := u.kube.GetIngress(ctx, GetIngressParam{NamespacedName: nsName})
	if err != nil {
		u.tel.Error(ctx, err, "Failed to get Ingress", "namespacedName", nsName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	ing := ha.GetNetworking().GetIngress()
	if !ing.IsEnabled() {
		if existing == nil {
			return ctrl.Result{}, nil
		}
		err := u.kube.DeleteIngress(ctx, DeleteIngressParam{NamespacedName: nsName})
		u.tel.IncIngressOperation(ctx, IncIngressOperationParam{NamespacedName: nsName, Operation: OperationDelete, Result: resultOf(err)})
		if err != nil {
			u.tel.Error(ctx, err, "Failed to delete Ingress", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Ingress deleted", "namespacedName", nsName)
		return ctrl.Result{}, nil
	}

	desired := buildIngress(ha, ing)
	if existing != nil {
		desired.ResourceVersion = existing.ResourceVersion
		err := u.kube.UpdateIngressOwnedByHermesAgent(ctx, UpdateIngressParam{HermesAgent: ha, Ingress: desired})
		u.tel.IncIngressOperation(ctx, IncIngressOperationParam{NamespacedName: nsName, Operation: OperationUpdate, Result: resultOf(err)})
		if err != nil {
			u.tel.Error(ctx, err, "Failed to update Ingress", "namespacedName", nsName)
			u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Ingress updated", "namespacedName", nsName)
		return ctrl.Result{}, nil
	}

	err = u.kube.CreateIngressOwnedByHermesAgent(ctx, CreateIngressOfHermesAgentParam{HermesAgent: ha, Ingress: desired})
	u.tel.IncIngressOperation(ctx, IncIngressOperationParam{NamespacedName: nsName, Operation: OperationCreate, Result: resultOf(err)})
	if err != nil {
		u.tel.Error(ctx, err, "Failed to create Ingress", "namespacedName", nsName)
		u.tel.IncReconcile(ctx, IncReconcileParam{NamespacedName: nsName, Result: ResultError})
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	u.tel.Debug(ctx, "Ingress created", "namespacedName", nsName)
	return ctrl.Result{}, nil
}

func buildIngress(ha *agentsv1alpha1.HermesAgent, ing *agentsv1alpha1.Ingress) *networkingv1.Ingress {
	rules := make([]networkingv1.IngressRule, 0, len(ing.Hosts))
	for _, h := range ing.Hosts {
		paths := buildIngressPaths(ha, h.Paths)
		rules = append(rules, networkingv1.IngressRule{
			Host: h.Host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{Paths: paths},
			},
		})
	}

	var tls []networkingv1.IngressTLS
	if len(ing.TLS) > 0 {
		tls = make([]networkingv1.IngressTLS, 0, len(ing.TLS))
		for _, t := range ing.TLS {
			tls = append(tls, networkingv1.IngressTLS{Hosts: t.Hosts, SecretName: t.SecretName})
		}
	}

	var annotations map[string]string
	if len(ing.Annotations) > 0 {
		annotations = maps.Clone(ing.Annotations)
	}

	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ha.Name,
			Namespace:   ha.Namespace,
			Labels:      resourceLabels(ha),
			Annotations: annotations,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ing.ClassName,
			Rules:            rules,
			TLS:              tls,
		},
	}
}

func buildIngressPaths(ha *agentsv1alpha1.HermesAgent, specPaths []agentsv1alpha1.IngressPath) []networkingv1.HTTPIngressPath {
	if len(specPaths) == 0 {
		specPaths = []agentsv1alpha1.IngressPath{{}}
	}

	paths := make([]networkingv1.HTTPIngressPath, 0, len(specPaths))
	for _, p := range specPaths {
		path := p.Path
		if path == "" {
			path = "/"
		}
		pathType := networkingv1.PathType(p.PathType)
		if p.PathType == "" {
			pathType = networkingv1.PathTypePrefix
		}
		port := hermesGatewayPort
		if p.Port != nil {
			port = *p.Port
		}
		paths = append(paths, networkingv1.HTTPIngressPath{
			Path:     path,
			PathType: &pathType,
			Backend: networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: ha.Name,
					Port: networkingv1.ServiceBackendPort{Number: port},
				},
			},
		})
	}
	return paths
}
