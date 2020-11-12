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

	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	cassdcapi "github.com/datastax/cass-operator/operator/pkg/apis/cassandra/v1beta1"
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
// +kubebuilder:rbac:groups=cassandra.datastax.com,namespace="medusa-operator",resources=cassandradatacenters,verbs=get;list;watch;create

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
				if  err = r.Status().Patch(ctx, restore, patch); err == nil {
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

		if isCassdcReady(cassdc) {
			r.Log.Info("the cassandradatacenter has been restored and is ready", "CassandraDatacenter", cassdcKey)

			patch := client.MergeFrom(restore.DeepCopy())
			restore.Status.FinishTime = metav1.Now()
			if  err = r.Status().Patch(ctx, restore, patch); err == nil {
				return ctrl.Result{Requeue: false}, err
			} else {
				r.Log.Error(err, "failed to patch status with end time")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, err
			}
		}

		// TODO handle scenarios in which the CassandraDatacenter fails to become ready

		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	backupKey := types.NamespacedName{Namespace: req.Namespace, Name: restore.Spec.Backup}
	backup := &api.CassandraBackup{}

	if err = r.Get(ctx, backupKey, backup); err != nil {
		// TODO add some additional logging and/or generate an event if the error is not found
		r.Log.Error(err, "failed to get backup", "CassandraBackup", backupKey)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

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

func buildNewCassandraDatacenter(restore *api.CassandraRestore, backup *api.CassandraBackup) (*cassdcapi.CassandraDatacenter, error) {
	newCassdc := &cassdcapi.CassandraDatacenter{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backup.Namespace,
			Name: restore.Spec.CassandraDatacenter.Name,
		},
		Spec: backup.Status.CassdcTemplateSpec.Spec,
	}

	index, err := getRestoreInitContainerIndex(newCassdc)
	if err != nil {
		return nil, err
	}

	restoreContainer := &newCassdc.Spec.PodTemplateSpec.Spec.InitContainers[index]
	envVars := restoreContainer.Env
	envVars = append(envVars, corev1.EnvVar{Name: "BACKUP_NAME", Value: backup.Name})
	restoreContainer.Env = envVars

	return newCassdc, nil
}

func getRestoreInitContainerIndex(cassdc *cassdcapi.CassandraDatacenter) (int, error) {
	initContainers := &cassdc.Spec.PodTemplateSpec.Spec.InitContainers

	for i, container := range *initContainers {
		if container.Name == restoreContainerName {
			return i, nil
		}
	}

	return 0, fmt.Errorf("restore initContainer (%s) not found")
}

func isCassdcReady(cassdc *cassdcapi.CassandraDatacenter) bool {
	if cassdc.Status.CassandraOperatorProgress != cassdcapi.ProgressReady {
		return false
	}
	status := cassdc.GetConditionStatus(cassdcapi.DatacenterReady)
	return status == corev1.ConditionTrue
}

func (r *CassandraRestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&api.CassandraRestore{}).
		Complete(r)
}
