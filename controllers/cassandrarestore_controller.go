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
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cassdcapi "github.com/datastax/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	"github.com/google/uuid"
	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
)

const (
	restoreContainerName = "medusa-restore"
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
// +kubebuilder:rbac:groups=cassandra.datastax.com,namespace="medusa-operator",resources=cassandradatacenters,verbs=get;list;watch;create;update

func (r *CassandraRestoreReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	_ = r.Log.WithValues("cassandrarestore", req.NamespacedName)

	instance := &api.CassandraRestore{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	restore := instance.DeepCopy()

	if len(restore.Status.RestoreKey) == 0 {
		if err = r.setRestoreKey(ctx, restore); err != nil {
			// Could be stale item, we'll just requeue - this process can be repeated
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	// See if the restore is already in progress
	if !restore.Status.StartTime.IsZero() {
		cassdcKey := types.NamespacedName{Namespace: req.Namespace, Name: restore.Spec.CassandraDatacenter.Name}
		cassdc := &cassdcapi.CassandraDatacenter{}

		if err = r.Get(ctx, cassdcKey, cassdc); err != nil {
			// TODO add some additional logging and/or generate an event if the error is not found
			if errors.IsNotFound(err) {
				r.Log.Error(err, "cassandradatacenter not found", "CassandraDatacenter", cassdcKey)

				patch := client.MergeFrom(restore.DeepCopy())
				restore.Status.FinishTime = metav1.Now()
				if err = r.Status().Patch(ctx, restore, patch); err == nil {
					return ctrl.Result{Requeue: false}, err
				} else {
					r.Log.Error(err, "failed to patch status with end time")
					return ctrl.Result{RequeueAfter: 5 * time.Second}, err
				}
			} else {
				r.Log.Error(err, "failed to get cassandradatacenter", "CassandraDatacenter", cassdcKey)
				return ctrl.Result{RequeueAfter: 10 * time.Second}, err
			}
		}

		if isCassdcReady(cassdc) && wasCassdcUpdated(restore.Status.StartTime.Time, cassdc) {
			r.Log.Info("the cassandradatacenter has been restored and is ready", "CassandraDatacenter", cassdcKey)

			patch := client.MergeFrom(restore.DeepCopy())
			restore.Status.FinishTime = metav1.Now()
			if err = r.Status().Patch(ctx, restore, patch); err == nil {
				return ctrl.Result{Requeue: false}, err
			} else {
				r.Log.Error(err, "failed to patch status with end time")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, err
			}
		}

		// TODO handle scenarios in which the CassandraDatacenter fails to become ready

		r.Log.Info("waiting for CassandraDatacenter to come online", "CassandraDatacenter", cassdcKey)
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	backupKey := types.NamespacedName{Namespace: req.Namespace, Name: restore.Spec.Backup}
	backup := &api.CassandraBackup{}

	if err = r.Get(ctx, backupKey, backup); err != nil {
		// TODO add some additional logging and/or generate an event if the error is not found
		r.Log.Error(err, "failed to get backup", "CassandraBackup", backupKey)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	if restore.Spec.InPlace {
		r.Log.Info("performing in place restore")

		cassdcKey := types.NamespacedName{Namespace: req.Namespace, Name: restore.Spec.CassandraDatacenter.Name}
		cassdc := &cassdcapi.CassandraDatacenter{}

		if err = r.Get(ctx, cassdcKey, cassdc); err != nil {
			r.Log.Error(err, "failed to get cassandradatacenter", "CassandraDatacenter", cassdcKey)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, err
		}

		cassdc = cassdc.DeepCopy()

		if restore.Spec.Shutdown {
			if cassdc.Spec.Stopped {
				// If cass-operator hasn't finished shutting down all the pods, requeue and check later again
				podList := &corev1.PodList{}
				r.List(ctx, podList, client.InNamespace(req.Namespace), client.MatchingLabels(map[string]string{"cassandra.datastax.com/datacenter": restore.Spec.CassandraDatacenter.Name}))

				if len(podList.Items) > 0 {
					// Some pods have not been shutdown yet
					return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
				}
			} else {
				cassdc.Spec.Stopped = true
				// Patch it
				if err = r.Update(ctx, cassdc); err != nil {
					r.Log.Error(err, "failed to update the cassandradatacenter", "CassandraDatacenter", cassdcKey)
					return ctrl.Result{RequeueAfter: 10 * time.Second}, err
				}

				// Wait for next time if it's ready
				r.Log.Info("the cassandradatacenter has been updated and will be shutdown", "CassandraDatacenter", cassdcKey)
				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
		}

		var podTemplateSpecUpdated bool
		if podTemplateSpecUpdated, err = setBackupNameInRestoreContainer(backup.Spec.Name, cassdc); err != nil {
			r.Log.Error(err, "failed to set backup name in restore container", "CassandraDatacenter", cassdcKey)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, err
		}

		if podTemplateSpecUpdated, err = setRestoreKeyInRestoreContainer(restore.Status.RestoreKey, cassdc); err != nil {
			r.Log.Error(err, "failed to set restore key in restore container", "CassandraDatacenter", cassdcKey)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, err
		}

		patch := client.MergeFromWithOptions(restore.DeepCopy(), client.MergeFromWithOptimisticLock{})
		restore.Status.StartTime = metav1.Now()
		if err = r.Status().Patch(ctx, restore, patch); err != nil {
			r.Log.Error(err, "fail to patch status with start time")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, err
		}

		if restore.Spec.Shutdown {
			if podTemplateSpecUpdated {
				r.Log.Info("updating racks", "CassandraDatacenter", cassdcKey)

				racks := make([]string, 0)
				for _, rack := range cassdc.Spec.Racks {
					racks = append(racks, rack.Name)
				}

				if err = r.Update(ctx, cassdc); err == nil {
					return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
				} else {
					r.Log.Error(err, "failed to force update racks", "CassandraDatacenter", cassdcKey)
					return ctrl.Result{RequeueAfter: 10 * time.Second}, err
				}
			} else {
				if isCassdcUpdating(cassdc) {
					r.Log.Info("waiting for rack updates to complete", "CassandraDatacenter", cassdcKey)
					return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
				}
			}

			// Restart the cluster
			cassdc.Spec.Stopped = false
			r.Log.Info("restarting the CassandraDatacenter", "CassandraDatacenter", cassdcKey)
		}

		if err = r.Update(ctx, cassdc); err == nil {
			r.Log.Info("the cassandradatacenter has been updated and will be restarted", "CassandraDatacenter", cassdcKey)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, err
		} else {
			r.Log.Error(err, "failed to update the cassandradatacenter", "CassandraDatacenter", cassdcKey)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, err
		}
	}

	r.Log.Info("restoring to new cassandradatacenter")

	newCassdc, err := buildNewCassandraDatacenter(restore, backup)
	if err != nil {
		r.Log.Error(err, "failed to build new cassandradatacenter")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	cassdcKey := types.NamespacedName{Namespace: newCassdc.Namespace, Name: newCassdc.Name}

	r.Log.Info("creating new cassandradatacenter", "CassandraDatacenter", cassdcKey)

	if err = r.Create(ctx, newCassdc); err == nil {
		patch := client.MergeFrom(restore.DeepCopy())
		restore.Status.StartTime = metav1.Now()
		if err = r.Status().Patch(ctx, restore, patch); err != nil {
			r.Log.Error(err, "fail to patch status with start time")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, err
		} else {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, err
		}
	} else {
		r.Log.Error(err, "failed to create cassandradatacenter", "CassandraDatacenter", cassdcKey)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}
}

func (r *CassandraRestoreReconciler) setRestoreKey(ctx context.Context, restore *api.CassandraRestore) error {
	key := uuid.New()
	patch := client.MergeFromWithOptions(restore.DeepCopy(), client.MergeFromWithOptimisticLock{})
	restore.Status.RestoreKey = key.String()

	return r.Status().Patch(ctx, restore, patch)
}

func buildNewCassandraDatacenter(restore *api.CassandraRestore, backup *api.CassandraBackup) (*cassdcapi.CassandraDatacenter, error) {
	newCassdc := &cassdcapi.CassandraDatacenter{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backup.Namespace,
			Name:      restore.Spec.CassandraDatacenter.Name,
		},
		Spec: backup.Status.CassdcTemplateSpec.Spec,
	}

	if _, err := setBackupNameInRestoreContainer(backup.Spec.Name, newCassdc); err != nil {
		return nil, err
	}

	if _, err := setRestoreKeyInRestoreContainer(restore.Status.RestoreKey, newCassdc); err != nil {
		return nil, err
	}

	return newCassdc, nil
}

func setBackupNameInRestoreContainer(backupName string, cassdc *cassdcapi.CassandraDatacenter) (bool, error) {
	index, err := getRestoreInitContainerIndex(cassdc)
	if err != nil {
		return false, err
	}

	updated := false
	restoreContainer := &cassdc.Spec.PodTemplateSpec.Spec.InitContainers[index]
	envVars := restoreContainer.Env
	envVarIdx := getEnvVarIndex("BACKUP_NAME", envVars)

	if envVarIdx > -1 {
		envVars[envVarIdx].Value = backupName
	} else {
		envVars = append(envVars, corev1.EnvVar{Name: "BACKUP_NAME", Value: backupName})
		updated = true
	}
	restoreContainer.Env = envVars

	return updated, nil
}

func setRestoreKeyInRestoreContainer(restoreKey string, cassdc *cassdcapi.CassandraDatacenter) (bool, error) {
	index, err := getRestoreInitContainerIndex(cassdc)
	if err != nil {
		return false, err
	}

	updated := false
	restoreContainer := &cassdc.Spec.PodTemplateSpec.Spec.InitContainers[index]
	envVars := restoreContainer.Env
	envVarIdx := getEnvVarIndex("RESTORE_KEY", envVars)

	if envVarIdx > -1 {
		envVars[envVarIdx].Value = restoreKey
	} else {
		envVars = append(envVars, corev1.EnvVar{Name: "RESTORE_KEY", Value: restoreKey})
		updated = true
	}
	restoreContainer.Env = envVars

	return updated, nil
}

func getRestoreInitContainerIndex(cassdc *cassdcapi.CassandraDatacenter) (int, error) {
	spec := cassdc.Spec.PodTemplateSpec
	initContainers := &spec.Spec.InitContainers

	for i, container := range *initContainers {
		if container.Name == restoreContainerName {
			return i, nil
		}
	}

	return 0, fmt.Errorf("restore initContainer (%s) not found", restoreContainerName)
}

func getEnvVarIndex(name string, envVars []corev1.EnvVar) int {
	for i, envVar := range envVars {
		if envVar.Name == name {
			return i
		}
	}
	return -1
}

func wasCassdcUpdated(startTime time.Time, cassdc *cassdcapi.CassandraDatacenter) bool {
	updateCondition, found := cassdc.GetCondition(cassdcapi.DatacenterUpdating)
	if !found || updateCondition.Status != corev1.ConditionFalse {
		return false
	}
	return updateCondition.LastTransitionTime.After(startTime)
}

func isCassdcReady(cassdc *cassdcapi.CassandraDatacenter) bool {
	if cassdc.Status.CassandraOperatorProgress != cassdcapi.ProgressReady {
		return false
	}

	statusReady := cassdc.GetConditionStatus(cassdcapi.DatacenterReady)
	return statusReady == corev1.ConditionTrue
}

func isCassdcUpdating(cassdc *cassdcapi.CassandraDatacenter) bool {
	statusUpdating := cassdc.GetConditionStatus(cassdcapi.DatacenterUpdating)
	return statusUpdating == corev1.ConditionTrue && cassdc.Status.CassandraOperatorProgress == cassdcapi.ProgressUpdating
}

func (r *CassandraRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&api.CassandraRestore{}).
		Complete(r)
}
