package usecase

import (
	"context"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (u *HermesAgentUseCase) reconcileRole(ctx context.Context, ha *agentsv1alpha1.HermesAgent) (ctrl.Result, error) {
	nsName := types.NamespacedName{Namespace: ha.Namespace, Name: ha.Name}

	existingRole, err := u.kube.GetRole(ctx, GetRoleParam{NamespacedName: nsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	existingRB, err := u.kube.GetRoleBinding(ctx, GetRoleBindingParam{NamespacedName: nsName})
	if err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	rules := ha.GetSecurity().GetRBAC().GetAdditionalRules()
	saName := ha.GetServiceAccountName()

	if len(rules) == 0 || saName == "" {
		if existingRB != nil {
			err := u.kube.DeleteRoleBinding(ctx, DeleteRoleBindingParam{NamespacedName: nsName})
			u.tel.IncRoleBindingOperation(ctx, IncRoleBindingOperationParam{NamespacedName: nsName, Operation: OperationDelete, Result: resultOf(err)})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "RoleBinding deleted", "namespacedName", nsName)
		}
		if existingRole != nil {
			err := u.kube.DeleteRole(ctx, DeleteRoleParam{NamespacedName: nsName})
			u.tel.IncRoleOperation(ctx, IncRoleOperationParam{NamespacedName: nsName, Operation: OperationDelete, Result: resultOf(err)})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "Role deleted", "namespacedName", nsName)
		}
		return ctrl.Result{}, nil
	}

	desiredRole := buildRole(ha, rules)
	if existingRole != nil {
		desiredRole.ResourceVersion = existingRole.ResourceVersion
		err := u.kube.UpdateRoleOwnedByHermesAgent(ctx, UpdateRoleParam{HermesAgent: ha, Role: desiredRole})
		u.tel.IncRoleOperation(ctx, IncRoleOperationParam{NamespacedName: nsName, Operation: OperationUpdate, Result: resultOf(err)})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Role updated", "namespacedName", nsName)
	} else {
		err := u.kube.CreateRoleOwnedByHermesAgent(ctx, CreateRoleOfHermesAgentParam{HermesAgent: ha, Role: desiredRole})
		u.tel.IncRoleOperation(ctx, IncRoleOperationParam{NamespacedName: nsName, Operation: OperationCreate, Result: resultOf(err)})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Role created", "namespacedName", nsName)
	}

	desiredRB := buildRoleBinding(ha, saName)
	if existingRB != nil {
		desiredRB.ResourceVersion = existingRB.ResourceVersion
		err := u.kube.UpdateRoleBindingOwnedByHermesAgent(ctx, UpdateRoleBindingParam{HermesAgent: ha, RoleBinding: desiredRB})
		u.tel.IncRoleBindingOperation(ctx, IncRoleBindingOperationParam{NamespacedName: nsName, Operation: OperationUpdate, Result: resultOf(err)})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "RoleBinding updated", "namespacedName", nsName)
	} else {
		err := u.kube.CreateRoleBindingOwnedByHermesAgent(ctx, CreateRoleBindingOfHermesAgentParam{HermesAgent: ha, RoleBinding: desiredRB})
		u.tel.IncRoleBindingOperation(ctx, IncRoleBindingOperationParam{NamespacedName: nsName, Operation: OperationCreate, Result: resultOf(err)})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "RoleBinding created", "namespacedName", nsName)
	}

	ha.Status.ManagedResources.Role = ha.Name
	ha.Status.ManagedResources.RoleBinding = ha.Name
	return ctrl.Result{}, nil
}

func buildRole(ha *agentsv1alpha1.HermesAgent, rules []agentsv1alpha1.RBACRule) *rbacv1.Role {
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
			Labels:    resourceLabels(ha),
		},
		Rules: policyRules,
	}
}

func buildRoleBinding(ha *agentsv1alpha1.HermesAgent, saName string) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.Name,
			Namespace: ha.Namespace,
			Labels:    resourceLabels(ha),
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
