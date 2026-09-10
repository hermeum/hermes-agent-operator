package usecase

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// silentTelemetry satisfies the Telemetry interface without side effects.
type silentTelemetry struct{}

func (silentTelemetry) Debug(context.Context, string, ...any)           {}
func (silentTelemetry) Info(context.Context, string, ...any)            {}
func (silentTelemetry) Error(context.Context, error, string, ...any)    {}
func (silentTelemetry) IncReconcile(context.Context, IncReconcileParam) {}
func (silentTelemetry) ObserveReconcileDuration(context.Context, ObserveReconcileDurationParam) {
}
func (silentTelemetry) IncNotFound(context.Context, IncNotFoundParam) {}

type fakeSnapshotKube struct {
	pvc       *corev1.PersistentVolumeClaim
	snapshots []VolumeSnapshot
	listErr   error
	created   []VolumeSnapshot
	deleted   []types.NamespacedName
	statuses  []*agentsv1alpha1.HermesAgent
	createErr error
}

func (f *fakeSnapshotKube) GetHermesAgent(ctx context.Context, param GetHermesAgentParam) (*agentsv1alpha1.HermesAgent, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) UpdateHermesAgentStatus(ctx context.Context, param UpdateHermesAgentStatusParam) error {
	f.statuses = append(f.statuses, param.HermesAgent)
	return nil
}
func (f *fakeSnapshotKube) GetPod(ctx context.Context, param GetPodParam) (*corev1.Pod, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) DeletePod(ctx context.Context, param DeletePodParam) error { return nil }
func (f *fakeSnapshotKube) GetConfigMap(ctx context.Context, param GetConfigMapParam) (*corev1.ConfigMap, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) CreateConfigMapOwnedByHermesAgent(ctx context.Context, param CreateConfigMapOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) UpdateConfigMapOwnedByHermesAgent(ctx context.Context, param UpdateConfigMapParam) error {
	return nil
}
func (f *fakeSnapshotKube) DeleteConfigMap(ctx context.Context, param DeleteConfigMapParam) error {
	return nil
}
func (f *fakeSnapshotKube) GetSecret(ctx context.Context, param GetSecretParam) (*corev1.Secret, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) CreateSecretOwnedByHermesAgent(ctx context.Context, param CreateSecretOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) UpdateSecretOwnedByHermesAgent(ctx context.Context, param UpdateSecretOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) DeleteSecret(ctx context.Context, param DeleteSecretParam) error {
	return nil
}
func (f *fakeSnapshotKube) GetStatefulSet(ctx context.Context, param GetStatefulSetParam) (*appsv1.StatefulSet, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) CreateStatefulSetOwnedByHermesAgent(ctx context.Context, param CreateStatefulSetOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) UpdateStatefulSetOwnedByHermesAgent(ctx context.Context, param UpdateStatefulSetParam) error {
	return nil
}
func (f *fakeSnapshotKube) DeleteStatefulSet(ctx context.Context, param DeleteStatefulSetParam) error {
	return nil
}
func (f *fakeSnapshotKube) GetServiceAccount(ctx context.Context, param GetServiceAccountParam) (*corev1.ServiceAccount, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) CreateServiceAccountOwnedByHermesAgent(ctx context.Context, param CreateServiceAccountOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) UpdateServiceAccountOwnedByHermesAgent(ctx context.Context, param UpdateServiceAccountParam) error {
	return nil
}
func (f *fakeSnapshotKube) DeleteServiceAccount(ctx context.Context, param DeleteServiceAccountParam) error {
	return nil
}
func (f *fakeSnapshotKube) GetRole(ctx context.Context, param GetRoleParam) (*rbacv1.Role, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) CreateRoleOwnedByHermesAgent(ctx context.Context, param CreateRoleOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) UpdateRoleOwnedByHermesAgent(ctx context.Context, param UpdateRoleParam) error {
	return nil
}
func (f *fakeSnapshotKube) DeleteRole(ctx context.Context, param DeleteRoleParam) error { return nil }
func (f *fakeSnapshotKube) GetRoleBinding(ctx context.Context, param GetRoleBindingParam) (*rbacv1.RoleBinding, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) CreateRoleBindingOwnedByHermesAgent(ctx context.Context, param CreateRoleBindingOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) UpdateRoleBindingOwnedByHermesAgent(ctx context.Context, param UpdateRoleBindingParam) error {
	return nil
}
func (f *fakeSnapshotKube) DeleteRoleBinding(ctx context.Context, param DeleteRoleBindingParam) error {
	return nil
}
func (f *fakeSnapshotKube) GetService(ctx context.Context, param GetServiceParam) (*corev1.Service, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) CreateServiceOwnedByHermesAgent(ctx context.Context, param CreateServiceOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) UpdateServiceOwnedByHermesAgent(ctx context.Context, param UpdateServiceParam) error {
	return nil
}
func (f *fakeSnapshotKube) DeleteService(ctx context.Context, param DeleteServiceParam) error {
	return nil
}
func (f *fakeSnapshotKube) GetIngress(ctx context.Context, param GetIngressParam) (*networkingv1.Ingress, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) CreateIngressOwnedByHermesAgent(ctx context.Context, param CreateIngressOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) UpdateIngressOwnedByHermesAgent(ctx context.Context, param UpdateIngressParam) error {
	return nil
}
func (f *fakeSnapshotKube) DeleteIngress(ctx context.Context, param DeleteIngressParam) error {
	return nil
}
func (f *fakeSnapshotKube) GetNetworkPolicy(ctx context.Context, param GetNetworkPolicyParam) (*networkingv1.NetworkPolicy, error) {
	return nil, nil
}
func (f *fakeSnapshotKube) CreateNetworkPolicyOwnedByHermesAgent(ctx context.Context, param CreateNetworkPolicyOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) UpdateNetworkPolicyOwnedByHermesAgent(ctx context.Context, param UpdateNetworkPolicyParam) error {
	return nil
}
func (f *fakeSnapshotKube) DeleteNetworkPolicy(ctx context.Context, param DeleteNetworkPolicyParam) error {
	return nil
}
func (f *fakeSnapshotKube) GetPersistentVolumeClaim(ctx context.Context, param GetPersistentVolumeClaimParam) (*corev1.PersistentVolumeClaim, error) {
	if f.pvc == nil {
		return nil, nil
	}
	return f.pvc, nil
}
func (f *fakeSnapshotKube) CreatePersistentVolumeClaimOwnedByHermesAgent(ctx context.Context, param CreatePersistentVolumeClaimOfHermesAgentParam) error {
	return nil
}
func (f *fakeSnapshotKube) GetVolumeSnapshot(ctx context.Context, param GetVolumeSnapshotParam) (*VolumeSnapshot, error) {
	for _, snap := range f.snapshots {
		if snap.Name == param.NamespacedName.Name && snap.Namespace == param.NamespacedName.Namespace {
			s := snap
			return &s, nil
		}
	}
	return nil, nil
}
func (f *fakeSnapshotKube) ListVolumeSnapshotsOwnedByAgent(ctx context.Context, param ListVolumeSnapshotsOwnedByAgentParam) ([]VolumeSnapshot, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.snapshots, nil
}

// snapshotAgentLabel mirrors the label applied by the real infra layer
// (infras.KubernetesClient.CreateVolumeSnapshotOwnedByHermesAgent).
const fakeSnapshotAgentLabel = "agents.hermeum.app/agent"

func (f *fakeSnapshotKube) CreateVolumeSnapshotOwnedByHermesAgent(ctx context.Context, param CreateVolumeSnapshotOfHermesAgentParam) error {
	if f.createErr != nil {
		return f.createErr
	}
	// Mimic the real infra: apply the agent attribution label before create,
	// and persist as the API server would (creationTimestamp stamped).
	obj := *param.VolumeSnapshot
	if obj.Labels == nil {
		obj.Labels = map[string]string{}
	}
	obj.Labels[fakeSnapshotAgentLabel] = param.HermesAgent.Name
	obj.CreationTimestamp = metav1.Time{Time: time.Now()}
	f.snapshots = append(f.snapshots, obj)
	f.created = append(f.created, obj)
	return nil
}
func (f *fakeSnapshotKube) DeleteVolumeSnapshot(ctx context.Context, param DeleteVolumeSnapshotParam) error {
	f.deleted = append(f.deleted, param.NamespacedName)
	return nil
}

// testPVCName is the existingClaim name used across the snapshot tests.
const testPVCName = "my-claim"

func snapshotHA(retention *int, schedule string) *agentsv1alpha1.HermesAgent {
	ha := minimalHA()
	ha.CreationTimestamp = metav1.NewTime(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Storage: &agentsv1alpha1.HermesStorage{
			Persistence: &agentsv1alpha1.HermesPersistence{Enabled: true},
			Snapshot: &agentsv1alpha1.HermesSnapshot{
				Enabled:   true,
				Schedule:  schedule,
				Retention: retention,
			},
		},
	}
	return ha
}

func boundPVC(name string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
}

func snapshotObj(name string, created time.Time) VolumeSnapshot {
	return VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: created},
		},
	}
}

func noMatchError() error {
	return &meta.NoKindMatchError{
		GroupKind:        schema.GroupKind{Group: "snapshot.storage.k8s.io", Kind: "VolumeSnapshot"},
		SearchedVersions: []string{"v1"},
	}
}

func TestReconcileSnapshot_DisabledIsNoOp(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: boundPVC("hermes-data-test-0")}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := minimalHA()
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero result, got %v", result)
	}
	if len(kube.created) != 0 {
		t.Errorf("expected no snapshots created, got %d", len(kube.created))
	}
}

func TestReconcileSnapshot_WaitsForNextRun(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: boundPVC("hermes-data-test-0")}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(nil, "0 3 * * *")
	ha.Status.Snapshot.LastScheduleTime = &metav1.Time{Time: time.Now().Add(-time.Hour)}
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Errorf("expected positive RequeueAfter, got %v", result.RequeueAfter)
	}
	if len(kube.created) != 0 {
		t.Errorf("expected no snapshot yet, got %d", len(kube.created))
	}
}

func TestReconcileSnapshot_CatchUpAfterMissedRun(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: boundPVC("hermes-data-test-0")}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(nil, "0 3 * * *")
	ha.Status.Snapshot.LastScheduleTime = &metav1.Time{Time: time.Now().Add(-48 * time.Hour)}
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(kube.created) != 1 {
		t.Fatalf("expected exactly 1 catch-up snapshot, got %d", len(kube.created))
	}
	name := kube.created[0].Name
	if want := fmt.Sprintf("hermes-data-%s-0-", ha.Name); len(name) <= len(want) || name[:len(want)] != want {
		t.Errorf("snapshot name %q should start with %q", name, want)
	}
	if ha.Status.Snapshot.LastScheduleTime == nil {
		t.Error("expected LastScheduleTime to be updated")
	}
	if len(ha.Status.Snapshot.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot in status, got %d", len(ha.Status.Snapshot.Snapshots))
	}
	ref := ha.Status.Snapshot.Snapshots[0]
	if ref.Name != name {
		t.Errorf("expected snapshot ref %q in status, got %q", name, ref.Name)
	}
	if ref.PVC != "hermes-data-test-0" {
		t.Errorf("expected snapshot ref PVC %q, got %q", "hermes-data-test-0", ref.PVC)
	}
	// Newest first: the just-created snapshot must be the list head.
	if len(ha.Status.Snapshot.Snapshots) > 0 && ha.Status.Snapshot.Snapshots[0].Name != name {
		t.Errorf("expected newest snapshot first, got %q", ha.Status.Snapshot.Snapshots[0].Name)
	}
	// Catch-up produces a single snapshot and requeues to the next slot.
	if result.RequeueAfter <= 0 {
		t.Errorf("expected positive RequeueAfter, got %v", result.RequeueAfter)
	}
	if len(kube.statuses) == 0 {
		t.Error("expected status update after snapshot")
	}
}

func TestReconcileSnapshot_FirstRunAnchorsToCreationTimestamp(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: boundPVC("hermes-data-test-0")}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(nil, "0 3 * * *")
	// Freshly created agent: next run is in the future, so no snapshot yet.
	ha.CreationTimestamp = metav1.Time{Time: time.Now()}
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(kube.created) != 0 {
		t.Errorf("expected no snapshot on first reconcile, got %d", len(kube.created))
	}
	if result.RequeueAfter <= 0 {
		t.Errorf("expected positive RequeueAfter, got %v", result.RequeueAfter)
	}
}

func TestReconcileSnapshot_SuspendedSkipsSnapshotting(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: boundPVC("hermes-data-test-0")}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(nil, "* * * * *")
	suspend := true
	ha.Spec.Suspend = &suspend
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero result, got %v", result)
	}
	if len(kube.created) != 0 {
		t.Errorf("expected no snapshots while suspended, got %d", len(kube.created))
	}
}

func TestReconcileSnapshot_UnboundPVCRequeues(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "hermes-data-test-0"},
		Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(nil, "* * * * *")
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s requeue, got %v", result.RequeueAfter)
	}
	if len(kube.created) != 0 {
		t.Errorf("expected no snapshots for unbound PVC, got %d", len(kube.created))
	}
}

func TestReconcileSnapshot_MissingCRDSetsCondition(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: boundPVC("hermes-data-test-0"), listErr: noMatchError()}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(nil, "* * * * *")
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("CRD absence must not surface as an error, got %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero result, got %v", result)
	}
	cond := meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionSnapshotUnsupported))
	if cond == nil {
		t.Fatal("expected SnapshotUnsupported condition")
	}
	if cond.Status != metav1.ConditionTrue || cond.Reason != condReasonCRDAbsent {
		t.Errorf("unexpected condition: %+v", cond)
	}
	if len(kube.created) != 0 {
		t.Errorf("expected no snapshots, got %d", len(kube.created))
	}
}

func TestReconcileSnapshot_InvalidScheduleSetsCondition(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: boundPVC("hermes-data-test-0")}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(nil, "not a cron")
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cond := meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionSnapshotUnsupported))
	if cond == nil {
		t.Fatal("expected SnapshotUnsupported condition")
	}
	if cond.Reason != condReasonInvalidSchedule {
		t.Errorf("unexpected reason: %s", cond.Reason)
	}
	if len(kube.created) != 0 {
		t.Errorf("expected no snapshots, got %d", len(kube.created))
	}
	_ = result
}

func TestReconcileSnapshot_StaleConditionCleared(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: boundPVC("hermes-data-test-0")}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(nil, "0 3 * * *")
	meta.SetStatusCondition(&ha.Status.Conditions, metav1.Condition{
		Type:   string(agentsv1alpha1.ConditionSnapshotUnsupported),
		Status: metav1.ConditionTrue,
		Reason: condReasonCRDAbsent,
	})
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionSnapshotUnsupported)) != nil {
		t.Error("expected stale SnapshotUnsupported condition to be removed")
	}
	_ = result
}

func TestReconcileSnapshot_RetentionKeepsNewest(t *testing.T) {
	ctx := context.Background()
	retention := 2
	kube := &fakeSnapshotKube{
		pvc: boundPVC("hermes-data-test-0"),
		snapshots: []VolumeSnapshot{
			snapshotObj("hermes-data-test-0-20260901030000", time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)),
			snapshotObj("hermes-data-test-0-20260902030000", time.Date(2026, 9, 2, 3, 0, 0, 0, time.UTC)),
			snapshotObj("hermes-data-test-0-20260903030000", time.Date(2026, 9, 3, 3, 0, 0, 0, time.UTC)),
		},
	}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(&retention, "0 3 * * *")
	// Schedule far in the future so only retention runs.
	ha.Status.Snapshot.LastScheduleTime = &metav1.Time{Time: time.Now().Add(-time.Hour)}
	if _, err := uc.reconcileSnapshot(ctx, ha); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(kube.deleted) != 1 {
		t.Fatalf("expected 1 deletion, got %d: %v", len(kube.deleted), kube.deleted)
	}
	if kube.deleted[0].Name != "hermes-data-test-0-20260901030000" {
		t.Errorf("expected oldest snapshot deleted, got %q", kube.deleted[0].Name)
	}
}

func TestBuildDataPVCName(t *testing.T) {
	t.Run("managed PVC uses volumeClaimTemplate name", func(t *testing.T) {
		ha := snapshotHA(nil, "0 3 * * *")
		if got := buildDataPVCName(ha); got != "hermes-data-test-0" {
			t.Errorf("buildDataPVCName() = %q, want %q", got, "hermes-data-test-0")
		}
	})

	t.Run("existingClaim wins", func(t *testing.T) {
		ha := snapshotHA(nil, "0 3 * * *")
		ha.Spec.Hermes.Storage.Persistence.ExistingClaim = ptrString(testPVCName)
		if got := buildDataPVCName(ha); got != testPVCName {
			t.Errorf("buildDataPVCName() = %q, want %q", got, testPVCName)
		}
	})

	t.Run("existingSnapshot restores into dedicated PVC", func(t *testing.T) {
		ha := snapshotHA(nil, "0 3 * * *")
		ha.Spec.Hermes.Storage.Persistence.ExistingSnapshot = ptrString("snap-1")
		if got := buildDataPVCName(ha); got != "snap-1-restore" {
			t.Errorf("buildDataPVCName() = %q, want %q", got, "snap-1-restore")
		}
	})

	t.Run("existingClaim wins over existingSnapshot", func(t *testing.T) {
		ha := snapshotHA(nil, "0 3 * * *")
		ha.Spec.Hermes.Storage.Persistence.ExistingSnapshot = ptrString("snap-1")
		ha.Spec.Hermes.Storage.Persistence.ExistingClaim = ptrString(testPVCName)
		if got := buildDataPVCName(ha); got != testPVCName {
			t.Errorf("buildDataPVCName() = %q, want %q", got, testPVCName)
		}
	})
}

func TestBuildSnapshotName(t *testing.T) {
	tm := time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC)
	if got := buildSnapshotName(testPVCName, tm); got != "my-claim-20260907030000" {
		t.Errorf("buildSnapshotName() = %q, want %q", got, "my-claim-20260907030000")
	}
}

// Snapshot names used across the split/mapping tests.
const (
	snapshotNameOld    = "old"
	snapshotNameMiddle = "middle"
	snapshotNameNew    = "new"
)

func TestSplitSnapshots(t *testing.T) {
	older := snapshotObj(snapshotNameOld, time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC))
	middle := snapshotObj(snapshotNameMiddle, time.Date(2026, 9, 2, 3, 0, 0, 0, time.UTC))
	newer := snapshotObj(snapshotNameNew, time.Date(2026, 9, 3, 3, 0, 0, 0, time.UTC))

	t.Run("all live when within retention", func(t *testing.T) {
		live, deprecated := splitSnapshots([]VolumeSnapshot{older, newer}, 2)
		if len(deprecated) != 0 {
			t.Errorf("expected no deprecated snapshots, got %v", deprecated)
		}
		if len(live) != 2 || live[0].Name != snapshotNameNew || live[1].Name != snapshotNameOld {
			t.Errorf("expected live sorted newest first, got %v", live)
		}
	})

	t.Run("newest kept, oldest deprecated", func(t *testing.T) {
		live, deprecated := splitSnapshots([]VolumeSnapshot{older, middle, newer}, 1)
		if len(live) != 1 || live[0].Name != snapshotNameNew {
			t.Errorf("expected only newest live, got %v", live)
		}
		if len(deprecated) != 2 || deprecated[0].Name != snapshotNameMiddle || deprecated[1].Name != snapshotNameOld {
			t.Errorf("expected deprecated sorted oldest last, got %v", deprecated)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		live, deprecated := splitSnapshots(nil, 3)
		if len(live) != 0 || len(deprecated) != 0 {
			t.Errorf("expected empty partitions, got live=%v deprecated=%v", live, deprecated)
		}
	})
}

func TestSnapshotRef(t *testing.T) {
	created := time.Date(2026, 9, 3, 3, 0, 0, 0, time.UTC)

	snap := snapshotObj("s1", created)
	snap.Spec.Source.PersistentVolumeClaimName = testPVCName
	ref := snapshotRef(snap)
	if ref.Name != "s1" || ref.PVC != testPVCName {
		t.Errorf("unexpected ref: %+v", ref)
	}
	if !ref.CreationTime.Time.Equal(created) {
		t.Errorf("creationTime = %v, want %v", ref.CreationTime.Time, created)
	}
}

func TestSnapshotRefs(t *testing.T) {
	older := snapshotObj(snapshotNameOld, time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC))
	older.Spec.Source.PersistentVolumeClaimName = testPVCName
	newer := snapshotObj(snapshotNameNew, time.Date(2026, 9, 3, 3, 0, 0, 0, time.UTC))
	newer.Spec.Source.PersistentVolumeClaimName = testPVCName

	refs := snapshotRefs([]VolumeSnapshot{newer, older})
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	if refs[0].Name != snapshotNameNew || refs[1].Name != snapshotNameOld {
		t.Errorf("expected input order preserved, got %v", refs)
	}

	t.Run("skips entries without name or creation time", func(t *testing.T) {
		nameless := VolumeSnapshot{Spec: VolumeSnapshotSpec{Source: VolumeSnapshotSource{PersistentVolumeClaimName: testPVCName}}}
		untimed := snapshotObj("untimed", time.Time{})
		if got := snapshotRefs([]VolumeSnapshot{nameless, untimed, older}); len(got) != 1 || got[0].Name != snapshotNameOld {
			t.Errorf("expected only valid entries, got %v", got)
		}
	})

	t.Run("skips snapshots being deleted", func(t *testing.T) {
		terminating := snapshotObj(snapshotNameMiddle, time.Date(2026, 9, 2, 3, 0, 0, 0, time.UTC))
		terminating.Spec.Source.PersistentVolumeClaimName = testPVCName
		now := metav1.Now()
		terminating.DeletionTimestamp = &now
		if got := snapshotRefs([]VolumeSnapshot{terminating, newer, older}); len(got) != 2 || got[0].Name != snapshotNameNew {
			t.Errorf("expected terminating snapshot skipped, got %v", got)
		}
	})
}

func TestBuildSnapshot(t *testing.T) {
	ha := snapshotHA(nil, "0 3 * * *")
	className := "fast-class"
	ha.Spec.Hermes.Storage.Snapshot.VolumeSnapshotClassName = &className
	ha.Spec.Hermes.Storage.Persistence.ExistingClaim = ptrString(testPVCName)

	name := buildSnapshotName(testPVCName, time.Date(2026, 9, 7, 3, 0, 0, 0, time.UTC))
	snap := buildSnapshot(ha, name)

	if snap.Name != name {
		t.Errorf("name = %q, want %q", snap.Name, name)
	}
	if snap.Namespace != ha.Namespace {
		t.Errorf("namespace = %q, want %q", snap.Namespace, ha.Namespace)
	}
	if got := snap.Spec.Source.PersistentVolumeClaimName; got != testPVCName {
		t.Errorf("source PVC = %q, want my-claim", got)
	}
	if got := snap.Spec.VolumeSnapshotClassName; got == nil || *got != "fast-class" {
		t.Errorf("volumeSnapshotClassName = %v, want fast-class", got)
	}
	// The agent attribution label is applied by the infra layer, not the builder.
	if snap.Labels != nil {
		t.Errorf("buildSnapshot should not set labels; attribution is applied by the infra layer, got %v", snap.Labels)
	}
	if len(snap.OwnerReferences) != 0 {
		t.Error("snapshots must not have ownerReferences")
	}

	// Omitting the class name must leave the field nil (cluster default applies).
	ha.Spec.Hermes.Storage.Snapshot.VolumeSnapshotClassName = nil
	snap = buildSnapshot(ha, name)
	if snap.Spec.VolumeSnapshotClassName != nil {
		t.Errorf("volumeSnapshotClassName should be nil when unset, got %v", snap.Spec.VolumeSnapshotClassName)
	}
}

func TestReconcileSnapshot_StatusTracksAllSnapshots(t *testing.T) {
	ctx := context.Background()
	retention := 2
	kube := &fakeSnapshotKube{
		pvc: boundPVC("hermes-data-test-0"),
		snapshots: []VolumeSnapshot{
			snapshotObj("hermes-data-test-0-20260901030000", time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)),
			snapshotObj("hermes-data-test-0-20260902030000", time.Date(2026, 9, 2, 3, 0, 0, 0, time.UTC)),
		},
	}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(&retention, "0 3 * * *")
	ha.Status.Snapshot.LastScheduleTime = &metav1.Time{Time: time.Now().Add(-48 * time.Hour)}
	if _, err := uc.reconcileSnapshot(ctx, ha); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Status contains all live snapshots (newest first) plus the new one.
	if len(ha.Status.Snapshot.Snapshots) != retention {
		t.Fatalf("expected %d snapshot refs in status, got %d", retention, len(ha.Status.Snapshot.Snapshots))
	}
	for i := 1; i < len(ha.Status.Snapshot.Snapshots); i++ {
		if ha.Status.Snapshot.Snapshots[i-1].CreationTime.Before(&ha.Status.Snapshot.Snapshots[i].CreationTime) {
			t.Errorf("status list not sorted newest first: %v before %v",
				ha.Status.Snapshot.Snapshots[i-1].CreationTime, ha.Status.Snapshot.Snapshots[i].CreationTime)
		}
	}
	// Retention deleted the oldest live snapshot (Sep 1).
	for _, ref := range ha.Status.Snapshot.Snapshots {
		if ref.Name == "hermes-data-test-0-20260901030000" {
			t.Errorf("retention-expired snapshot %q still in status", ref.Name)
		}
	}
}

func TestReconcileSnapshot_CreateConflictTolerated(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{
		pvc:       boundPVC("hermes-data-test-0"),
		createErr: apierrors.NewAlreadyExists(schema.GroupResource{Group: "snapshot.storage.k8s.io", Resource: "volumesnapshots"}, "x"),
	}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})

	ha := snapshotHA(nil, "0 3 * * *")
	ha.Status.Snapshot.LastScheduleTime = &metav1.Time{Time: time.Now().Add(-48 * time.Hour)}
	result, err := uc.reconcileSnapshot(ctx, ha)
	if err != nil {
		t.Fatalf("AlreadyExists must not surface as error, got %v", err)
	}
	if ha.Status.Snapshot.LastScheduleTime == nil {
		t.Error("expected LastScheduleTime to advance even on conflict")
	}
	if result.RequeueAfter <= 0 {
		t.Errorf("expected positive RequeueAfter, got %v", result.RequeueAfter)
	}
}

func TestReconcileSnapshot_SnapshotNotOwned(t *testing.T) {
	ctx := context.Background()
	kube := &fakeSnapshotKube{pvc: boundPVC(testPVCName)}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := snapshotHA(nil, "0 3 * * *")
	ha.Spec.Hermes.Storage.Persistence.ExistingClaim = ptrString(testPVCName)
	ha.Status.Snapshot.LastScheduleTime = &metav1.Time{Time: time.Now().Add(-48 * time.Hour)}

	if _, err := uc.reconcileSnapshot(ctx, ha); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(kube.created) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(kube.created))
	}
	created := kube.created[0]
	if !strings.HasPrefix(created.Name, "my-claim-") {
		t.Errorf("existingClaim should be used for the snapshot name, got %q", created.Name)
	}
	if created.Labels[fakeSnapshotAgentLabel] != ha.Name {
		t.Errorf("expected agent label %q, got %v", ha.Name, created.Labels)
	}
	if len(created.OwnerReferences) != 0 {
		t.Error("snapshots must not have ownerReferences")
	}
}
