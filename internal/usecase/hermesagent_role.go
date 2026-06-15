package usecase

import (
	"context"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "RoleBinding deleted", "namespacedName", nsName)
		}
		if existingRole != nil {
			err := u.kube.DeleteRole(ctx, DeleteRoleParam{NamespacedName: nsName})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "Role deleted", "namespacedName", nsName)
		}
		ha.Status.ManagedResources.Role = ""
		ha.Status.ManagedResources.RoleBinding = ""
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		return ctrl.Result{}, nil
	}

	roleCreated := existingRole == nil
	desiredRole := buildRole(ha, rules)
	if existingRole != nil {
		if !roleEqual(desiredRole, existingRole) {
			desiredRole.ResourceVersion = existingRole.ResourceVersion
			err := u.kube.UpdateRoleOwnedByHermesAgent(ctx, UpdateRoleParam{HermesAgent: ha, Role: desiredRole})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "Role updated", "namespacedName", nsName)
		}
	} else {
		err := u.kube.CreateRoleOwnedByHermesAgent(ctx, CreateRoleOfHermesAgentParam{HermesAgent: ha, Role: desiredRole})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "Role created", "namespacedName", nsName)
	}

	rbCreated := existingRB == nil
	desiredRB := buildRoleBinding(ha, saName)
	if existingRB != nil {
		if !roleBindingEqual(desiredRB, existingRB) {
			desiredRB.ResourceVersion = existingRB.ResourceVersion
			err := u.kube.UpdateRoleBindingOwnedByHermesAgent(ctx, UpdateRoleBindingParam{HermesAgent: ha, RoleBinding: desiredRB})
			if err != nil {
				return ctrl.Result{RequeueAfter: 30 * time.Second}, err
			}
			u.tel.Debug(ctx, "RoleBinding updated", "namespacedName", nsName)
		}
	} else {
		err := u.kube.CreateRoleBindingOwnedByHermesAgent(ctx, CreateRoleBindingOfHermesAgentParam{HermesAgent: ha, RoleBinding: desiredRB})
		if err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
		u.tel.Debug(ctx, "RoleBinding created", "namespacedName", nsName)
	}

	if roleCreated || rbCreated {
		ha.Status.ManagedResources.Role = ha.Name
		ha.Status.ManagedResources.RoleBinding = ha.Name
		if err := u.kube.UpdateHermesAgentStatus(ctx, UpdateHermesAgentStatusParam{HermesAgent: ha}); err != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, err
		}
	}
	return ctrl.Result{}, nil
}

func roleEqual(a, b *rbacv1.Role) bool {
	return equality.Semantic.DeepEqual(a.Rules, b.Rules)
}

func roleBindingEqual(a, b *rbacv1.RoleBinding) bool {
	return equality.Semantic.DeepEqual(a.Subjects, b.Subjects) && a.RoleRef == b.RoleRef
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
