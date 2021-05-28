package controllers

import (
	"context"
	cassdcapi "github.com/k8ssandra/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
	"time"
)

func testInPlaceRestore(t *testing.T, ctx context.Context, namespace string) {
	restore := &api.CassandraRestore{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "test-restore",
		},
		Spec: api.CassandraRestoreSpec{
			Backup:   "test-backup",
			Shutdown: true,
			InPlace:  true,
			CassandraDatacenter: api.CassandraDatacenterConfig{
				Name:        TestCassandraDatacenterName,
				ClusterName: "test-dc",
			},
		},
	}

	err := testClient.Create(ctx, restore)
	require.NoError(t, err, "failed to create CassandraRestore")

	dcKey := types.NamespacedName{Namespace: "default", Name: TestCassandraDatacenterName}

	t.Log("check that the datacenter is set to be stopped")
	require.Eventually(t, func() bool {
		dc := &cassdcapi.CassandraDatacenter{}
		err := testClient.Get(ctx, dcKey, dc)
		if err != nil {
			return false
		}
		return dc.Spec.Stopped == true
	}, timeout, interval, "timed out waiting for CassandraDatacenter stopped flag to be set")

	t.Log("delete datacenter pods to simulate shutdown")
	err = testClient.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace("default"), client.MatchingLabels{cassdcapi.DatacenterLabel: TestCassandraDatacenterName})
	require.NoError(t, err, "failed to delete datacenter pods")

	t.Log("check that the datacenter podTemplateSpec is updated")
	require.Eventually(t, func() bool {
		dc := &cassdcapi.CassandraDatacenter{}
		err := testClient.Get(ctx, dcKey, dc)
		if err != nil {
			return false
		}

		restoreContainer := findContainer(dc.Spec.PodTemplateSpec.Spec.InitContainers, "medusa-restore")
		if restoreContainer == nil {
			return false
		}

		envVar := findEnvVar(restoreContainer.Env, "BACKUP_NAME")
		if envVar == nil || envVar.Value != "test-backup" {
			return false
		}

		envVar = findEnvVar(restoreContainer.Env, "RESTORE_KEY")
		return envVar != nil
	}, timeout, interval, "timed out waiting for CassandraDatacenter PodTemplateSpec update")

	t.Log("check datacenter force update racks")
	require.Eventually(t, func() bool {
		dc := &cassdcapi.CassandraDatacenter{}
		err := testClient.Get(ctx, dcKey, dc)
		if err != nil {
			return false
		}
		return len(dc.Spec.ForceUpgradeRacks) == 1 && dc.Spec.ForceUpgradeRacks[0] == "rack1"
	}, timeout, interval)

	t.Log("check restore status start time set")
	require.Eventually(t, func() bool {
		restore := &api.CassandraRestore{}
		err := testClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-restore"}, restore)
		if err != nil {
			return false
		}

		return !restore.Status.StartTime.IsZero()
	}, timeout, interval)

	t.Log("check datacenter restarted")
	require.Eventually(t, func() bool {
		dc := &cassdcapi.CassandraDatacenter{}
		err := testClient.Get(ctx, dcKey, dc)
		if err != nil {
			return false
		}
		return !dc.Spec.Stopped
	}, timeout, interval)

	t.Log("set datacenter status to updated and ready")
	dc := &cassdcapi.CassandraDatacenter{}
	err = testClient.Get(ctx, dcKey, dc)
	require.NoError(t, err, "failed to get datacenter")

	restore = &api.CassandraRestore{}
	err = testClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-restore"}, restore)
	require.NoError(t, err, "failed to get restore object")

	dc.SetCondition(cassdcapi.DatacenterCondition{
		Type:               cassdcapi.DatacenterUpdating,
		Status:             corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(restore.Status.StartTime.Time.Add(10 * time.Second)),
	})
	dc.SetCondition(cassdcapi.DatacenterCondition{
		Type:               cassdcapi.DatacenterReady,
		Status:             corev1.ConditionTrue,
		LastTransitionTime: metav1.Now(),
	})
	dc.Status.CassandraOperatorProgress = cassdcapi.ProgressReady
	dc.Status.SuperUserUpserted = metav1.Now()
	dc.Status.UsersUpserted = metav1.Now()
	dc.Status.LastServerNodeStarted = metav1.Now()
	dc.Status.LastRollingRestart = metav1.Now()
	dc.Status.NodeStatuses = cassdcapi.CassandraStatusMap{}
	dc.Status.NodeReplacements = []string{}
	dc.Status.QuietPeriod = metav1.Now()

	err = testClient.Status().Update(ctx, dc)
	require.NoError(t, err, "failed to set datacenter status to updated and ready")

	t.Log("check restore status finish time set")
	require.Eventually(t, func() bool {
		restore := &api.CassandraRestore{}
		err := testClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "test-restore"}, restore)
		if err != nil {
			return false
		}

		return restore.Status.FinishTime.After(restore.Status.StartTime.Time)
	}, timeout, interval)
}

func findContainer(containers []corev1.Container, name string) *corev1.Container {
	for _, container := range containers {
		if container.Name == name {
			return &container
		}
	}
	return nil
}

func findEnvVar(envVars []corev1.EnvVar, name string) *corev1.EnvVar {
	for _, envVar := range envVars {
		if envVar.Name == name {
			return &envVar
		}
	}
	return nil
}
