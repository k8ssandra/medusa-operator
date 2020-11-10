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
	"github.com/k8ssandra/medusa-operator/pkg/medusa"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cassdcapi "github.com/datastax/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	operrors "github.com/k8ssandra/medusa-operator/pkg/errors"
	corev1 "k8s.io/api/core/v1"
)

const(
	backupSidecarPort = 50051
	backupSidecarName = "medusa"
)

// CassandraBackupReconciler reconciles a CassandraBackup object
type CassandraBackupReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cassandra.k8ssandra.io,namespace="medusa-operator",resources=cassandrabackups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cassandra.k8ssandra.io,namespace="medusa-operator",resources=cassandrabackups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cassandra.datastax.com,namespace="medusa-operator",resources=cassandradatacenters,verbs=get;list;watch
// +kubebuilder:rbac:groups="batch",namespace="medusa-operator",resources=jobs,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",namespace="medusa-operator",resources=pods;services,verbs=get;list;watch

func (r *CassandraBackupReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	_ = r.Log.WithValues("cassandrabackup", req.NamespacedName)

	instance := &api.CassandraBackup{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	backup := instance.DeepCopy()

	if backupFinished(backup) {
		return ctrl.Result{Requeue: false}, nil
	}

	// First check to see if the backup is already in progress
	if !backup.Status.StartTime.IsZero() {
		// If there is anything in progress, simply requeue the request
		if len(backup.Status.InProgress) > 0 {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}

		r.Log.Info("backup complete")

		// Set the finish time
		// Note that the time here is not accurate, but that is ok. For now we are just
		// using it as a completion marker.
		patch := client.MergeFrom(backup.DeepCopy())
		backup.Status.FinishTime = metav1.Now()
		if err := r.Status().Patch(context.Background(), backup, patch); err != nil {
			r.Log.Error(err, "failed to patch status with finish time")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, err
		}

		return ctrl.Result{Requeue: false}, nil
	}

	cassdcKey := types.NamespacedName{Namespace: backup.Namespace, Name: backup.Spec.CassandraDatacenter}
	cassdc := &cassdcapi.CassandraDatacenter{}
	err = r.Get(ctx, cassdcKey, cassdc)
	if err != nil {
		r.Log.Error(err, "failed to get cassandradatacenter", "CassandraDatacenter", cassdcKey)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	pods, err := r.getCassandraDatacenterPods(ctx, cassdc)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	// Make sure that Medusa is deployed
	if !isMedusaDeployed(pods) {
		r.Log.Error(operrors.BackupSidecarNotFound, "medusa is not deployed", "CassandraDatacenter", cassdcKey)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, operrors.BackupSidecarNotFound
	}

	patch := client.MergeFrom(backup.DeepCopy())
	backup.Status.StartTime = metav1.Now()
	for _, pod := range pods {
		backup.Status.InProgress = append(backup.Status.InProgress, pod.Name)
	}
	if err := r.Status().Patch(context.Background(), backup, patch); err != nil {
		r.Log.Error(err, "failed to patch status with backup start time", "Backup", backup)
		return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, err
	}

	for _, p := range pods {
		go func(pod corev1.Pod) {
			r.Log.Info("starting backup", "CassandraPod", pod.Name)
			succeeded := false
			if err := doBackup(ctx, backup.Spec.Name, &pod); err == nil {
				r.Log.Info("finished backup", "CassandraPod", pod.Name)
				succeeded = true
			} else {
				r.Log.Error(err, "backup failed", "CassandraPod", pod.Name)
			}
			patch := client.MergeFrom(backup.DeepCopy())
			backup.Status.InProgress = removeValue(backup.Status.InProgress, pod.Name)
			if succeeded {
				backup.Status.Finished = append(backup.Status.Finished, pod.Name)
			} else {
				backup.Status.Failed = append(backup.Status.Failed, pod.Name)
			}
			if err := r.Status().Patch(context.Background(), backup, patch); err != nil {
				r.Log.Error(err, "failed to patch status", "Backup", backup)
			}
		}(p)
	}

	return ctrl.Result{}, nil
}

func (r *CassandraBackupReconciler) getCassandraDatacenterPods(ctx context.Context, cassdc *cassdcapi.CassandraDatacenter) ([]corev1.Pod, error) {
	cassdcSvc := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Namespace: cassdc.Namespace,  Name: cassdc.GetAllPodsServiceName()}, cassdcSvc)
	if err != nil {
		return nil, err
	}

	podList := &corev1.PodList{}
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: cassdcSvc.Spec.Selector})
	listOpts := []client.ListOption{
		client.MatchingLabelsSelector{
			Selector: selector,
		},
	}

	if err := r.List(context.Background(), podList, listOpts...); err != nil {
		r.Log.Error(err, "failed to get pods for cassandradatacenter", "CassandraDatacenter", cassdc.Name)
		return nil, err
	}

	pods := make([]corev1.Pod, 0)
	for _, pod := range podList.Items {
		pods = append(pods, pod)
	}

	return pods, nil
}

func isMedusaDeployed(pods []corev1.Pod) bool {
	for _, pod := range pods {
		if !hasMedusaSidecar(&pod) {
			return false
		}
	}
	return true
}

func hasMedusaSidecar(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == backupSidecarName {
			return true
		}
	}
	return false
}

func doBackup(ctx context.Context, name string, pod *corev1.Pod) error {
	addr := fmt.Sprintf("%s:%d", pod.Status.PodIP, backupSidecarPort)
	if medusaClient, err := medusa.NewClient(addr); err != nil {
		return err
	} else {
		defer medusaClient.Close()
		return medusaClient.CreateBackup(ctx, name)
	}
}

func backupFinished(backup *api.CassandraBackup) bool {
	return !backup.Status.FinishTime.IsZero()
}

func removeValue(slice []string, value string) []string {
	newSlice := make([]string, 0)
	for _, s := range slice {
		if s != value {
			newSlice = append(newSlice, s)
		}
	}
	return newSlice
}

func (r *CassandraBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&api.CassandraBackup{}).
		Complete(r)
}
