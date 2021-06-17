/*


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

package controllers

import (
	"context"
	"fmt"
	"github.com/k8ssandra/medusa-operator/pkg/cassandra"
	"k8s.io/apimachinery/pkg/api/errors"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/uuid"
	cassdcapi "github.com/k8ssandra/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	"github.com/k8ssandra/medusa-operator/pkg/reconcile"
)

const (
	restoreContainerName = "medusa-restore"
	backupNameEnvVar     = "BACKUP_NAME"
	restoreKeyEnvVar     = "RESTORE_KEY"
)

// CassandraRestoreReconciler reconciles a CassandraRestore object
type CassandraRestoreReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cassandra.k8ssandra.io,namespace="medusa-operator",resources=cassandrarestores,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cassandra.k8ssandra.io,namespace="medusa-operator",resources=cassandrarestores/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cassandra.k8ssandra.io,namespace="medusa-operator",resources=cassandrarebackups,verbs=get;list;watch
// +kubebuilder:rbac:groups=cassandra.datastax.com,namespace="medusa-operator",resources=cassandradatacenters,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=apps,namespace="medusa-operator",resources=statefulsets,verbs=list;watch

func (r *CassandraRestoreReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()

	factory := reconcile.NewFactory(r.Client, r.Log)
	request, result, err := factory.NewRestoreRequest(ctx, req.NamespacedName)

	if result != nil {
		return *result, err
	}

	request.SetRestoreStartTime(metav1.Now())
	request.SetRestoreKey(uuid.New().String())

	if request.Restore.Spec.Shutdown && request.Restore.Status.DatacenterStopped.IsZero() {
		if stopped := stopDatacenter(request); !stopped {
			return r.applyUpdatesAndRequeue(ctx, request)
		}
	}

	if err := updateRestoreInitContainer(request); err != nil {
		request.Log.Error(err, "The datacenter is not properly configured for backup/restore")
		// No need to requeue here because the datacenter is not properly configured for
		// backup/restore with Medusa.
		return ctrl.Result{}, err
	}

	complete, err := r.podTemplateSpecUpdateComplete(ctx, request)

	if err != nil {
		request.Log.Error(err, "Failed to check if datacenter update is complete")
		// Not going to bother applying updates here since we hit an error.
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	if !complete {
		request.Log.Info("Waiting for datacenter update to complete")
		return r.applyUpdatesAndRequeue(ctx, request)
	}

	request.Log.Info("The datacenter has been updated")

	if request.Datacenter.Spec.Stopped {
		request.Log.Info("Starting the datacenter")
		request.Datacenter.Spec.Stopped = false

		return r.applyUpdatesAndRequeue(ctx, request)
	}

	if !cassandra.DatacenterReady(request.Datacenter) {
		request.Log.Info("Waiting for datacenter to come back online")
		return r.applyUpdatesAndRequeue(ctx, request)
	}

	request.SetRestoreFinishTime(metav1.Now())
	if err := r.applyUpdates(ctx, request); err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	request.Log.Info("The restore operation is complete")
	return ctrl.Result{}, nil
}

// applyUpdates patches the CassandraDatacenter if its spec has been updated and patches
// the CassandraRestore if its status has been updated.
func (r *CassandraRestoreReconciler) applyUpdates(ctx context.Context, req *reconcile.RestoreRequest) error {
	if req.DatacenterModified() {
		if err := r.Patch(ctx, req.Datacenter, req.GetDatacenterPatch()); err != nil {
			if errors.IsResourceExpired(err) {
				req.Log.Info("CassandraDatacenter version expired!")
			}
			req.Log.Error(err, "Failed to patch the CassandraDatacenter")
			return err
		}
	}

	if req.RestoreModified() {
		if err := r.Status().Patch(ctx, req.Restore, req.GetRestorePatch()); err != nil {
			req.Log.Error(err, "Failed to patch the CassandraRestore")
			return err
		}
	}

	return nil
}

func (r *CassandraRestoreReconciler) applyUpdatesAndRequeue(ctx context.Context, req *reconcile.RestoreRequest) (ctrl.Result, error) {
	if err := r.applyUpdates(ctx, req); err != nil {
		req.Log.Error(err, "Failed to apply updates")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// updateRestoreInitContainer sets the backup name and restore key env vars in the restore
// init container. An error is returned if the container is not found.
func updateRestoreInitContainer(req *reconcile.RestoreRequest) error {
	if err := setBackupNameInRestoreContainer(req.Backup.Spec.Name, req.Datacenter); err != nil {
		return err
	}
	return setRestoreKeyInRestoreContainer(req.Restore.Status.RestoreKey, req.Datacenter)
}

// podTemplateSpecUpdateComplete checks that the pod template spec changes, namely the ones
// with the restore container, have been pushed down to the StatefulSets. Return true if
// the changes have been applied.
func (r *CassandraRestoreReconciler) podTemplateSpecUpdateComplete(ctx context.Context, req *reconcile.RestoreRequest) (bool, error) {
	if updated := cassandra.DatacenterUpdatedAfter(req.Restore.Status.DatacenterStopped.Time, req.Datacenter); !updated {
		return false, nil
	}

	// It may not be sufficient to check for the update only via status conditions. We will
	// check the template spec of the StatefulSets to be certain that the update has been
	// applied. We do this in order to avoid an extra rolling restart after the
	// StatefulSets are scaled back up.

	statefulsetList := &appsv1.StatefulSetList{}
	labels := client.MatchingLabels{cassdcapi.ClusterLabel: req.Datacenter.Spec.ClusterName, cassdcapi.DatacenterLabel: req.Datacenter.Name}

	if err := r.List(ctx, statefulsetList, labels); err != nil {
		req.Log.Error(err, "Failed to get StatefulSets")
		return false, err
	}

	for _, statefulset := range statefulsetList.Items {
		container := getRestoreInitContainerFromStatefulSet(&statefulset)

		if container == nil {
			return false, nil
		}

		if !containerHasEnvVar(container, backupNameEnvVar, req.Backup.Spec.Name) {
			return false, nil
		}

		if !containerHasEnvVar(container, restoreKeyEnvVar, req.Restore.Status.RestoreKey) {
			return false, nil
		}
	}

	return true, nil
}

// stopDatacenter sets the Stopped property in the Datacenter spec to true. Returns true if
// the datacenter is stopped.
func stopDatacenter(req *reconcile.RestoreRequest) bool {
	if cassandra.DatacenterStopped(req.Datacenter) {
		req.Log.Info("The datacenter is stopped", "Status", req.Datacenter.Status)
		req.SetDatacenterStoppedTime(metav1.Now())
		return true
	}

	if cassandra.DatacenterStopping(req.Datacenter) {
		req.Log.Info("Waiting for datacenter to stop")
		return false
	}

	req.Log.Info("Stopping datacenter")
	req.Datacenter.Spec.Stopped = true
	return false
}


func buildNewCassandraDatacenter(restore *api.CassandraRestore, backup *api.CassandraBackup) (*cassdcapi.CassandraDatacenter, error) {
	newCassdc := &cassdcapi.CassandraDatacenter{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backup.Namespace,
			Name:      restore.Spec.CassandraDatacenter.Name,
		},
		Spec: backup.Status.CassdcTemplateSpec.Spec,
	}

	if err := setBackupNameInRestoreContainer(backup.Spec.Name, newCassdc); err != nil {
		return nil, err
	}

	if err := setRestoreKeyInRestoreContainer(restore.Status.RestoreKey, newCassdc); err != nil {
		return nil, err
	}

	return newCassdc, nil
}

func setBackupNameInRestoreContainer(backupName string, cassdc *cassdcapi.CassandraDatacenter) error {
	index, err := getRestoreInitContainerIndex(cassdc)
	if err != nil {
		return err
	}

	restoreContainer := &cassdc.Spec.PodTemplateSpec.Spec.InitContainers[index]
	envVars := restoreContainer.Env
	envVarIdx := getEnvVarIndex(backupNameEnvVar, envVars)

	if envVarIdx > -1 {
		envVars[envVarIdx].Value = backupName
	} else {
		envVars = append(envVars, corev1.EnvVar{Name: backupNameEnvVar, Value: backupName})
	}
	restoreContainer.Env = envVars

	return nil
}

func setRestoreKeyInRestoreContainer(restoreKey string, dc *cassdcapi.CassandraDatacenter) error {
	index, err := getRestoreInitContainerIndex(dc)
	if err != nil {
		return err
	}

	restoreContainer := &dc.Spec.PodTemplateSpec.Spec.InitContainers[index]
	envVars := restoreContainer.Env
	envVarIdx := getEnvVarIndex(restoreKeyEnvVar, envVars)

	if envVarIdx > -1 {
		envVars[envVarIdx].Value = restoreKey
	} else {
		envVars = append(envVars, corev1.EnvVar{Name: restoreKeyEnvVar, Value: restoreKey})
	}
	restoreContainer.Env = envVars

	return nil
}

func getRestoreInitContainerIndex(dc *cassdcapi.CassandraDatacenter) (int, error) {
	spec := dc.Spec.PodTemplateSpec
	initContainers := &spec.Spec.InitContainers

	for i, container := range *initContainers {
		if container.Name == restoreContainerName {
			return i, nil
		}
	}

	return 0, fmt.Errorf("restore initContainer (%s) not found", restoreContainerName)
}

func containerHasEnvVar(container *corev1.Container, name, value string) bool {
	idx := getEnvVarIndex(name, container.Env)

	if idx < 0 {
		return false
	}

	return container.Env[idx].Value == value
}

func getEnvVarIndex(name string, envVars []corev1.EnvVar) int {
	for i, envVar := range envVars {
		if envVar.Name == name {
			return i
		}
	}
	return -1
}

func getRestoreInitContainerFromStatefulSet(statefulset *appsv1.StatefulSet) *corev1.Container {
	for _, container := range statefulset.Spec.Template.Spec.InitContainers {
		if container.Name == restoreContainerName {
			return &container
		}
	}
	return nil
}

func (r *CassandraRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&api.CassandraRestore{}).
		Complete(r)
}

