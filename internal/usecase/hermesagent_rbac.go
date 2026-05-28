package usecase

import (
	"context"
	"maps"
	agentsv1alpha1 "noahingh/hermes-agent-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (u *HermesAgentUseCase) reconcileServiceAccount(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	name := ha.Name
	nsName := types.NamespacedName{Name: name, Namespace: ha.Namespace}

	existing, err := u.kube.GetServiceAccount(ctx, GetServiceAccountParam{NamespacedName: nsName})
	if err != nil {
		return err
	}

	if !ha.GetSecurity().GetRBAC().ShouldCreateServiceAccount() {
		if existing == nil {
			return nil
		}
		err := u.kube.DeleteServiceAccount(ctx, DeleteServiceAccountParam{NamespacedName: nsName})
		u.tel.IncServiceAccountOperation(ctx, IncServiceAccountOperationParam{Operation: OperationDelete, Result: resultOf(err)})
		return err
	}

	desired := u.buildServiceAccount(ha)
	if existing != nil {
		desired.ResourceVersion = existing.ResourceVersion
		err := u.kube.UpdateServiceAccountOwnedByHermesAgent(ctx, UpdateServiceAccountParam{HermesAgent: ha, ServiceAccount: desired})
		u.tel.IncServiceAccountOperation(ctx, IncServiceAccountOperationParam{Operation: OperationUpdate, Result: resultOf(err)})
		return err
	}

	err = u.kube.CreateServiceAccountOwnedByHermesAgent(ctx, CreateServiceAccountOfHermesAgentParam{HermesAgent: ha, ServiceAccount: desired})
	u.tel.IncServiceAccountOperation(ctx, IncServiceAccountOperationParam{Operation: OperationCreate, Result: resultOf(err)})
	return err
}

func (u *HermesAgentUseCase) buildServiceAccount(ha *agentsv1alpha1.HermesAgent) *corev1.ServiceAccount {
	var annotations map[string]string
	if r := ha.GetSecurity().GetRBAC(); r != nil && len(r.ServiceAccountAnnotations) > 0 {
		annotations = make(map[string]string, len(r.ServiceAccountAnnotations))
		maps.Copy(annotations, r.ServiceAccountAnnotations)
	}
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ha.Name,
			Namespace:   ha.Namespace,
			Annotations: annotations,
		},
	}
}

func (u *HermesAgentUseCase) reconcileRole(ctx context.Context, ha *agentsv1alpha1.HermesAgent) error {
	name := ha.Name
	nsName := types.NamespacedName{Name: name, Namespace: ha.Namespace}

	existingRole, err := u.kube.GetRole(ctx, GetRoleParam{NamespacedName: nsName})
	if err != nil {
		return err
	}
	existingRB, err := u.kube.GetRoleBinding(ctx, GetRoleBindingParam{NamespacedName: nsName})
	if err != nil {
		return err
	}

	rules := ha.GetSecurity().GetRBAC().GetAdditionalRules()
	saName := ha.GetServiceAccountName()

	if len(rules) == 0 || saName == "" {
		if existingRB != nil {
			err := u.kube.DeleteRoleBinding(ctx, DeleteRoleBindingParam{NamespacedName: nsName})
			u.tel.IncRoleBindingOperation(ctx, IncRoleBindingOperationParam{Operation: OperationDelete, Result: resultOf(err)})
			if err != nil {
				return err
			}
		}
		if existingRole != nil {
			err := u.kube.DeleteRole(ctx, DeleteRoleParam{NamespacedName: nsName})
			u.tel.IncRoleOperation(ctx, IncRoleOperationParam{Operation: OperationDelete, Result: resultOf(err)})
			if err != nil {
				return err
			}
		}
		return nil
	}

	desiredRole := u.buildRole(ha, rules)
	if existingRole != nil {
		desiredRole.ResourceVersion = existingRole.ResourceVersion
		err := u.kube.UpdateRoleOwnedByHermesAgent(ctx, UpdateRoleParam{HermesAgent: ha, Role: desiredRole})
		u.tel.IncRoleOperation(ctx, IncRoleOperationParam{Operation: OperationUpdate, Result: resultOf(err)})
		if err != nil {
			return err
		}
	} else {
		err := u.kube.CreateRoleOwnedByHermesAgent(ctx, CreateRoleOfHermesAgentParam{HermesAgent: ha, Role: desiredRole})
		u.tel.IncRoleOperation(ctx, IncRoleOperationParam{Operation: OperationCreate, Result: resultOf(err)})
		if err != nil {
			return err
		}
	}

	desiredRB := u.buildRoleBinding(ha, saName)
	if existingRB != nil {
		desiredRB.ResourceVersion = existingRB.ResourceVersion
		err := u.kube.UpdateRoleBindingOwnedByHermesAgent(ctx, UpdateRoleBindingParam{HermesAgent: ha, RoleBinding: desiredRB})
		u.tel.IncRoleBindingOperation(ctx, IncRoleBindingOperationParam{Operation: OperationUpdate, Result: resultOf(err)})
		return err
	}
	err = u.kube.CreateRoleBindingOwnedByHermesAgent(ctx, CreateRoleBindingOfHermesAgentParam{HermesAgent: ha, RoleBinding: desiredRB})
	u.tel.IncRoleBindingOperation(ctx, IncRoleBindingOperationParam{Operation: OperationCreate, Result: resultOf(err)})
	return err
}

func (u *HermesAgentUseCase) buildRole(ha *agentsv1alpha1.HermesAgent, rules []agentsv1alpha1.RBACRule) *rbacv1.Role {
	policyRules := make([]rbacv1.PolicyRule, 0, len(rules))
	for _, r := range rules {
		policyRules = append(policyRules, rbacv1.PolicyRule{
			APIGroups: append([]string(nil), r.APIGroups...),
			Resources: append([]string(nil), r.Resources...),
			Verbs:     append([]string(nil), r.Verbs...),
		})
	}
	return &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.Name,
			Namespace: ha.Namespace,
		},
		Rules: policyRules,
	}
}

func (u *HermesAgentUseCase) buildRoleBinding(ha *agentsv1alpha1.HermesAgent, saName string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.Name,
			Namespace: ha.Namespace,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      saName,
				Namespace: ha.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     ha.Name,
		},
	}
}
