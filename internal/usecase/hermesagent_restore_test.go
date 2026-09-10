package usecase

import (
	"context"
	"testing"
	"time"

	agentsv1alpha1 "hermeum/hermes-agent-operator/api/v1alpha1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// fakeRestoreKube is a fake Kubernetes interface for restore tests. PVCs,
// snapshots, and the StatefulSet are keyed by name.
type fakeRestoreKube struct {
	pvcs         map[string]*corev1.PersistentVolumeClaim
	snapshots    map[string]*VolumeSnapshot
	sts          *appsv1.StatefulSet
	stsDeletes   int
	createdPVCs  []*corev1.PersistentVolumeClaim
	createdSnaps []VolumeSnapshot
	deletedSnaps []string
	statuses     []*agentsv1alpha1.HermesAgent
	getSnapErr   error
}

func newFakeRestoreKube() *fakeRestoreKube {
	return &fakeRestoreKube{
		pvcs:      map[string]*corev1.PersistentVolumeClaim{},
		snapshots: map[string]*VolumeSnapshot{},
	}
}

func (f *fakeRestoreKube) GetHermesAgent(ctx context.Context, param GetHermesAgentParam) (*agentsv1alpha1.HermesAgent, error) {
	return nil, nil
}
func (f *fakeRestoreKube) UpdateHermesAgentStatus(ctx context.Context, param UpdateHermesAgentStatusParam) error {
	f.statuses = append(f.statuses, param.HermesAgent)
	return nil
}
func (f *fakeRestoreKube) GetPod(ctx context.Context, param GetPodParam) (*corev1.Pod, error) {
	return nil, nil
}
func (f *fakeRestoreKube) DeletePod(ctx context.Context, param DeletePodParam) error { return nil }

func (f *fakeRestoreKube) GetConfigMap(ctx context.Context, param GetConfigMapParam) (*corev1.ConfigMap, error) {
	return nil, nil
}
func (f *fakeRestoreKube) CreateConfigMapOwnedByHermesAgent(ctx context.Context, param CreateConfigMapOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) UpdateConfigMapOwnedByHermesAgent(ctx context.Context, param UpdateConfigMapParam) error {
	return nil
}
func (f *fakeRestoreKube) DeleteConfigMap(ctx context.Context, param DeleteConfigMapParam) error {
	return nil
}

func (f *fakeRestoreKube) GetSecret(ctx context.Context, param GetSecretParam) (*corev1.Secret, error) {
	return nil, nil
}
func (f *fakeRestoreKube) CreateSecretOwnedByHermesAgent(ctx context.Context, param CreateSecretOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) UpdateSecretOwnedByHermesAgent(ctx context.Context, param UpdateSecretOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) DeleteSecret(ctx context.Context, param DeleteSecretParam) error {
	return nil
}

func (f *fakeRestoreKube) GetStatefulSet(ctx context.Context, param GetStatefulSetParam) (*appsv1.StatefulSet, error) {
	if f.sts == nil {
		return nil, nil
	}
	return f.sts, nil
}
func (f *fakeRestoreKube) CreateStatefulSetOwnedByHermesAgent(ctx context.Context, param CreateStatefulSetOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) UpdateStatefulSetOwnedByHermesAgent(ctx context.Context, param UpdateStatefulSetParam) error {
	return nil
}
func (f *fakeRestoreKube) DeleteStatefulSet(ctx context.Context, param DeleteStatefulSetParam) error {
	f.stsDeletes++
	f.sts = nil
	return nil
}

func (f *fakeRestoreKube) GetServiceAccount(ctx context.Context, param GetServiceAccountParam) (*corev1.ServiceAccount, error) {
	return nil, nil
}
func (f *fakeRestoreKube) CreateServiceAccountOwnedByHermesAgent(ctx context.Context, param CreateServiceAccountOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) UpdateServiceAccountOwnedByHermesAgent(ctx context.Context, param UpdateServiceAccountParam) error {
	return nil
}
func (f *fakeRestoreKube) DeleteServiceAccount(ctx context.Context, param DeleteServiceAccountParam) error {
	return nil
}

func (f *fakeRestoreKube) GetRole(ctx context.Context, param GetRoleParam) (*rbacv1.Role, error) {
	return nil, nil
}
func (f *fakeRestoreKube) CreateRoleOwnedByHermesAgent(ctx context.Context, param CreateRoleOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) UpdateRoleOwnedByHermesAgent(ctx context.Context, param UpdateRoleParam) error {
	return nil
}
func (f *fakeRestoreKube) DeleteRole(ctx context.Context, param DeleteRoleParam) error { return nil }

func (f *fakeRestoreKube) GetRoleBinding(ctx context.Context, param GetRoleBindingParam) (*rbacv1.RoleBinding, error) {
	return nil, nil
}
func (f *fakeRestoreKube) CreateRoleBindingOwnedByHermesAgent(ctx context.Context, param CreateRoleBindingOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) UpdateRoleBindingOwnedByHermesAgent(ctx context.Context, param UpdateRoleBindingParam) error {
	return nil
}
func (f *fakeRestoreKube) DeleteRoleBinding(ctx context.Context, param DeleteRoleBindingParam) error {
	return nil
}

func (f *fakeRestoreKube) GetService(ctx context.Context, param GetServiceParam) (*corev1.Service, error) {
	return nil, nil
}
func (f *fakeRestoreKube) CreateServiceOwnedByHermesAgent(ctx context.Context, param CreateServiceOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) UpdateServiceOwnedByHermesAgent(ctx context.Context, param UpdateServiceParam) error {
	return nil
}
func (f *fakeRestoreKube) DeleteService(ctx context.Context, param DeleteServiceParam) error {
	return nil
}

func (f *fakeRestoreKube) GetIngress(ctx context.Context, param GetIngressParam) (*networkingv1.Ingress, error) {
	return nil, nil
}
func (f *fakeRestoreKube) CreateIngressOwnedByHermesAgent(ctx context.Context, param CreateIngressOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) UpdateIngressOwnedByHermesAgent(ctx context.Context, param UpdateIngressParam) error {
	return nil
}
func (f *fakeRestoreKube) DeleteIngress(ctx context.Context, param DeleteIngressParam) error {
	return nil
}

func (f *fakeRestoreKube) GetNetworkPolicy(ctx context.Context, param GetNetworkPolicyParam) (*networkingv1.NetworkPolicy, error) {
	return nil, nil
}
func (f *fakeRestoreKube) CreateNetworkPolicyOwnedByHermesAgent(ctx context.Context, param CreateNetworkPolicyOfHermesAgentParam) error {
	return nil
}
func (f *fakeRestoreKube) UpdateNetworkPolicyOwnedByHermesAgent(ctx context.Context, param UpdateNetworkPolicyParam) error {
	return nil
}
func (f *fakeRestoreKube) DeleteNetworkPolicy(ctx context.Context, param DeleteNetworkPolicyParam) error {
	return nil
}

func (f *fakeRestoreKube) GetPersistentVolumeClaim(ctx context.Context, param GetPersistentVolumeClaimParam) (*corev1.PersistentVolumeClaim, error) {
	pvc, ok := f.pvcs[param.NamespacedName.Name]
	if !ok {
		return nil, nil
	}
	return pvc, nil
}
func (f *fakeRestoreKube) CreatePersistentVolumeClaimOwnedByHermesAgent(ctx context.Context, param CreatePersistentVolumeClaimOfHermesAgentParam) error {
	name := param.PersistentVolumeClaim.Name
	if _, exists := f.pvcs[name]; exists {
		return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "persistentvolumeclaims"}, name)
	}
	f.pvcs[name] = param.PersistentVolumeClaim
	f.createdPVCs = append(f.createdPVCs, param.PersistentVolumeClaim)
	return nil
}

func (f *fakeRestoreKube) GetVolumeSnapshot(ctx context.Context, param GetVolumeSnapshotParam) (*VolumeSnapshot, error) {
	if f.getSnapErr != nil {
		return nil, f.getSnapErr
	}
	snap, ok := f.snapshots[param.NamespacedName.Name]
	if !ok {
		return nil, nil
	}
	return snap, nil
}
func (f *fakeRestoreKube) ListVolumeSnapshotsOwnedByAgent(ctx context.Context, param ListVolumeSnapshotsOwnedByAgentParam) ([]VolumeSnapshot, error) {
	var out []VolumeSnapshot
	for _, snap := range f.snapshots {
		if snap.Labels[fakeSnapshotAgentLabel] == param.AgentName {
			out = append(out, *snap)
		}
	}
	return out, nil
}
func (f *fakeRestoreKube) CreateVolumeSnapshotOwnedByHermesAgent(ctx context.Context, param CreateVolumeSnapshotOfHermesAgentParam) error {
	name := param.VolumeSnapshot.Name
	if _, exists := f.snapshots[name]; exists {
		return apierrors.NewAlreadyExists(schema.GroupResource{Group: "snapshot.storage.k8s.io", Resource: "volumesnapshots"}, name)
	}
	obj := *param.VolumeSnapshot
	if obj.Labels == nil {
		obj.Labels = map[string]string{}
	}
	obj.Labels[fakeSnapshotAgentLabel] = param.HermesAgent.Name
	obj.CreationTimestamp = metav1.Time{Time: time.Now()}
	f.snapshots[name] = &obj
	f.createdSnaps = append(f.createdSnaps, obj)
	return nil
}
func (f *fakeRestoreKube) DeleteVolumeSnapshot(ctx context.Context, param DeleteVolumeSnapshotParam) error {
	if _, ok := f.snapshots[param.NamespacedName.Name]; ok {
		f.deletedSnaps = append(f.deletedSnaps, param.NamespacedName.Name)
		delete(f.snapshots, param.NamespacedName.Name)
	}
	return nil
}

// restoreHA builds a HermesAgent with persistence (enabled PVC) and an
// existingSnapshot source.
func restoreHA(source string) *agentsv1alpha1.HermesAgent {
	ha := minimalHA()
	ha.Spec.Hermes = &agentsv1alpha1.Hermes{
		Storage: &agentsv1alpha1.HermesStorage{
			Persistence: &agentsv1alpha1.HermesPersistence{
				Enabled:          true,
				ExistingSnapshot: ptrString(source),
			},
		},
	}
	return ha
}

// readySnapshot returns a ReadyToUse VolumeSnapshot with the given restoreSize.
func readySnapshot(name string, size string) *VolumeSnapshot {
	ready := true
	return &VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Status: &VolumeSnapshotStatus{
			ReadyToUse:  &ready,
			RestoreSize: resourceQty(size),
		},
	}
}

func resourceQty(s string) *resource.Quantity {
	q := resource.MustParse(s)
	return &q
}

// Shared names across the restore tests.
const (
	restoreSourceSnapshot = "snap-20260909"
	restoreRestoredPVC    = "snap-20260909-restore"
	restoreOldPVCName     = "hermes-data-test-0"

	simpleSnapshot    = "snap-1"
	simpleRestoredPVC = "snap-1-restore"
	oldStorageClass   = "old-class"
)

// vctStatefulSet builds a StatefulSet that provisions hermes-data via a
// volumeClaimTemplate (the shape the reshape gate must remove).
func vctStatefulSet() *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: hermesHomeVolume}},
			},
		},
	}
}

func TestReconcileRestore_FieldUnsetIsNoOp(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := restoreHA("")
	meta.SetStatusCondition(&ha.Status.Conditions, metav1.Condition{
		Type:   string(agentsv1alpha1.ConditionRestoreFailed),
		Status: metav1.ConditionTrue,
		Reason: condReasonSnapshotNotReady,
	})

	result, err := uc.reconcileRestore(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero result, got %v", result)
	}
	if meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionRestoreFailed)) != nil {
		t.Error("stale RestoreFailed condition must be cleared")
	}
	if len(kube.createdPVCs) != 0 || kube.stsDeletes != 0 {
		t.Error("nothing must be created or deleted when the field is unset")
	}
}

func TestReconcileRestore_MissingSnapshotSetsCondition(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.pvcs[restoreOldPVCName] = boundPVC(restoreOldPVCName)
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := restoreHA("gone-snap")

	result, err := uc.reconcileRestore(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s requeue, got %v", result.RequeueAfter)
	}
	cond := meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionRestoreFailed))
	if cond == nil {
		t.Fatal("expected RestoreFailed condition")
	}
	if cond.Reason != condReasonSnapshotNotReady {
		t.Errorf("reason = %q, want %q", cond.Reason, condReasonSnapshotNotReady)
	}
	if len(kube.createdPVCs) != 0 || kube.stsDeletes != 0 {
		t.Error("nothing must be provisioned for a missing snapshot")
	}
}

func TestReconcileRestore_NotReadySnapshotSetsCondition(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.pvcs[restoreOldPVCName] = boundPVC(restoreOldPVCName)
	kube.snapshots["pending-snap"] = &VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{Name: "pending-snap", Namespace: "default"},
		Status:     &VolumeSnapshotStatus{ReadyToUse: ptrBool(false)},
	}
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := restoreHA("pending-snap")

	if _, err := uc.reconcileRestore(ctx, ha); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cond := meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionRestoreFailed))
	if cond == nil {
		t.Fatal("expected RestoreFailed condition")
	}
	if len(kube.createdPVCs) != 0 || kube.stsDeletes != 0 {
		t.Error("nothing must be provisioned for a not-ready snapshot")
	}
}

func TestReconcileRestore_MissingCRDSetsCondition(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.getSnapErr = noMatchError()
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := restoreHA("snap-1")

	result, err := uc.reconcileRestore(ctx, ha)
	if err != nil {
		t.Fatalf("CRD absence must not surface as an error, got %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s requeue, got %v", result.RequeueAfter)
	}
	cond := meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionRestoreFailed))
	if cond == nil || cond.Reason != condReasonSnapshotCRDAbsent {
		t.Fatalf("expected CRD-absent condition, got %+v", cond)
	}
}

func TestReconcileRestore_ProvisionsPVCWhileAgentRuns(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.pvcs[restoreOldPVCName] = boundPVC(restoreOldPVCName)
	kube.snapshots[restoreSourceSnapshot] = readySnapshot(restoreSourceSnapshot, "15Gi")
	kube.sts = vctStatefulSet()
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := restoreHA(restoreSourceSnapshot)

	// First reconcile: the restore PVC is created (unbound) while the old
	// volume, pod, and StatefulSet are untouched.
	if _, err := uc.reconcileRestore(ctx, ha); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRestoredPVC(t, kube, restoreSourceSnapshot, "15Gi")
	if kube.stsDeletes != 0 {
		t.Error("StatefulSet must not be deleted before the restore PVC is bound")
	}
	if _, ok := kube.pvcs[restoreOldPVCName]; !ok {
		t.Error("the old data PVC must be left in place")
	}

	// PVC binds; second reconcile reshapes: deletes the VCT-shaped
	// StatefulSet once so the next pass recreates it with the explicit volume.
	kube.pvcs[restoreRestoredPVC].Status.Phase = corev1.ClaimBound
	result, err := uc.reconcileRestore(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected requeue after reshape, got %v", result)
	}
	if kube.stsDeletes != 1 {
		t.Errorf("expected StatefulSet deleted once, got %d", kube.stsDeletes)
	}

	// Third reconcile: no StatefulSet, nothing left to do — zero result lets
	// reconcileStatefulSet recreate it with the restored volume.
	result, err = uc.reconcileRestore(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero result, got %v", result)
	}
	if kube.stsDeletes != 1 || len(kube.createdPVCs) != 1 {
		t.Errorf("restore must be idempotent; stsDeletes=%d createdPVCs=%d", kube.stsDeletes, len(kube.createdPVCs))
	}
	if meta.FindStatusCondition(ha.Status.Conditions, string(agentsv1alpha1.ConditionRestoreFailed)) != nil {
		t.Error("RestoreFailed condition must be cleared on success")
	}
	// The source snapshot is never deleted.
	if len(kube.deletedSnaps) != 0 {
		t.Errorf("no snapshots must be deleted, got %v", kube.deletedSnaps)
	}
	if _, ok := kube.snapshots[restoreSourceSnapshot]; !ok {
		t.Error("source snapshot must be preserved")
	}
}

func TestReconcileRestore_UnboundPVCRequeues(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.snapshots[simpleSnapshot] = readySnapshot(simpleSnapshot, "10Gi")
	kube.sts = vctStatefulSet()
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := restoreHA(simpleSnapshot)

	// PVC created but pending: no teardown, no completion.
	if _, err := uc.reconcileRestore(ctx, ha); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	result, err := uc.reconcileRestore(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s requeue while unbound, got %v", result.RequeueAfter)
	}
	if kube.stsDeletes != 0 {
		t.Error("StatefulSet must not be deleted while the restore PVC is unbound")
	}
}

func TestReconcileRestore_ExplicitVolumeStatefulSetNotDeleted(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.snapshots[simpleSnapshot] = readySnapshot(simpleSnapshot, "10Gi")
	// A StatefulSet already shaped with an explicit volume (e.g. from a
	// previous restore) must not be deleted again.
	kube.sts = &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{{Name: hermesHomeVolume}},
				},
			},
		},
	}
	kube.pvcs[simpleRestoredPVC] = boundPVC(simpleRestoredPVC)
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := restoreHA(simpleSnapshot)

	result, err := uc.reconcileRestore(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero result, got %v", result)
	}
	if kube.stsDeletes != 0 {
		t.Error("an explicit-volume StatefulSet must not be deleted")
	}
}

func TestReconcileRestore_AlreadyProvisionedIsNoOp(t *testing.T) {
	ctx := context.Background()
	kube := newFakeRestoreKube()
	kube.snapshots[simpleSnapshot] = readySnapshot(simpleSnapshot, "10Gi")
	kube.pvcs[simpleRestoredPVC] = boundPVC(simpleRestoredPVC)
	uc := NewHermesAgentUseCase(kube, silentTelemetry{})
	ha := restoreHA(simpleSnapshot)

	result, err := uc.reconcileRestore(ctx, ha)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsZero() {
		t.Errorf("expected zero result, got %v", result)
	}
	if len(kube.createdPVCs) != 0 {
		t.Errorf("existing restore PVC must be reused, got %d creates", len(kube.createdPVCs))
	}
}

func TestReconcileRestore_StorageClass(t *testing.T) {
	ctx := context.Background()

	t.Run("spec storageClassName is honored", func(t *testing.T) {
		kube := newFakeRestoreKube()
		kube.snapshots[simpleSnapshot] = readySnapshot(simpleSnapshot, "10Gi")
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := restoreHA(simpleSnapshot)
		specClass := "spec-class"
		ha.Spec.Hermes.Storage.Persistence.StorageClassName = &specClass

		if _, err := uc.reconcileRestore(ctx, ha); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := kube.createdPVCs[0].Spec.StorageClassName
		if got == nil || *got != "spec-class" {
			t.Errorf("storageClassName = %v, want spec-class", got)
		}
	})

	t.Run("cluster default when unset", func(t *testing.T) {
		kube := newFakeRestoreKube()
		kube.snapshots[simpleSnapshot] = readySnapshot(simpleSnapshot, "10Gi")
		uc := NewHermesAgentUseCase(kube, silentTelemetry{})
		ha := restoreHA(simpleSnapshot)

		if _, err := uc.reconcileRestore(ctx, ha); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := kube.createdPVCs[0].Spec.StorageClassName; got != nil {
			t.Errorf("storageClassName = %v, want nil (cluster default)", got)
		}
	})
}

func TestBuildRestoredPVCName(t *testing.T) {
	if got := buildRestoredPVCName(simpleSnapshot); got != simpleRestoredPVC {
		t.Errorf("buildRestoredPVCName() = %q, want %q", got, simpleRestoredPVC)
	}
}

func TestStatefulSetHasVolumeClaimTemplate(t *testing.T) {
	if !statefulSetHasVolumeClaimTemplate(vctStatefulSet(), hermesHomeVolume) {
		t.Error("expected VCT-shaped StatefulSet detected")
	}
	explicit := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{{Name: hermesHomeVolume}},
				},
			},
		},
	}
	if statefulSetHasVolumeClaimTemplate(explicit, hermesHomeVolume) {
		t.Error("explicit-volume StatefulSet must not be detected as VCT-shaped")
	}
}

func TestBuildRestoredPVC(t *testing.T) {
	ha := restoreHA(simpleSnapshot)
	specClass := oldStorageClass
	ha.Spec.Hermes.Storage.Persistence.StorageClassName = &specClass
	snap := readySnapshot(simpleSnapshot, "20Gi")

	pvc := buildRestoredPVC(ha, snap)
	if pvc.Name != simpleRestoredPVC || pvc.Namespace != "default" {
		t.Errorf("unexpected object meta: %s/%s", pvc.Namespace, pvc.Name)
	}
	if got := pvc.Spec.Resources.Requests.Storage().String(); got != "20Gi" {
		t.Errorf("size = %q, want 20Gi", got)
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != oldStorageClass {
		t.Errorf("storageClassName = %v, want %s", pvc.Spec.StorageClassName, oldStorageClass)
	}
	if len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != corev1.ReadWriteOnce {
		t.Errorf("accessModes = %v, want [ReadWriteOnce]", pvc.Spec.AccessModes)
	}

	// Nil restoreSize falls back to the persistence default.
	snapNilSize := readySnapshot(simpleSnapshot, "10Gi")
	snapNilSize.Status.RestoreSize = nil
	pvc = buildRestoredPVC(ha, snapNilSize)
	if got := pvc.Spec.Resources.Requests.Storage().String(); got != "10Gi" {
		t.Errorf("size = %q, want 10Gi default", got)
	}
}

// assertRestoredPVC checks the shape of the PVC provisioned from a snapshot.
func assertRestoredPVC(t *testing.T, kube *fakeRestoreKube, snapshotName, size string) {
	t.Helper()
	if len(kube.createdPVCs) != 1 {
		t.Fatalf("expected restore PVC created, got %d", len(kube.createdPVCs))
	}
	restored := kube.createdPVCs[0]
	if restored.Name != snapshotName+"-restore" {
		t.Errorf("restore PVC name = %q, want %q-restore", restored.Name, snapshotName)
	}
	if restored.Spec.DataSource == nil || restored.Spec.DataSource.Name != snapshotName ||
		restored.Spec.DataSource.Kind != "VolumeSnapshot" || restored.Spec.DataSource.APIGroup == nil || *restored.Spec.DataSource.APIGroup != "snapshot.storage.k8s.io" {
		t.Errorf("unexpected dataSource: %+v", restored.Spec.DataSource)
	}
	if got := restored.Spec.Resources.Requests.Storage().String(); got != size {
		t.Errorf("restore PVC size = %q, want %q (from snapshot restoreSize)", got, size)
	}
}

// Compile-time interface checks for the fakes.
var (
	_ Kubernetes = (*fakeRestoreKube)(nil)
	_ Kubernetes = (*fakeSnapshotKube)(nil)
)
