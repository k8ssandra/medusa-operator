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
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	cassdcapi "github.com/datastax/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	v1batch "k8s.io/api/batch/v1"
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

	cassdc := &cassdcapi.CassandraDatacenter{}
	err = r.Get(ctx, types.NamespacedName{Namespace: backup.Namespace, Name: backup.Spec.CassandraDatacenter}, cassdc)
	if err != nil {
		r.Log.Error(err, "failed to get cassandradatacenter", "CassandraDatacenter", backup.Spec.CassandraDatacenter)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	pods, err := r.getCassandraDatacenterPods(ctx, cassdc)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}

	grpcServices, err := getGrpcServiceAddresses(&pods)
	if err != nil {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}



	backupJob := &v1batch.Job{}
	if err = r.Client.Get(ctx, req.NamespacedName, backupJob); err == nil {
		// The job has already been created, so we are done.
		return ctrl.Result{}, nil
	} else if errors.IsNotFound(err) {
		backupJob = newBackupJob(backup, grpcServices)
		r.Log.Info("creating backup job", "jobName", backupJob.Name)
		if err = r.Client.Create(ctx, backupJob); err == nil {
			// The job was created. We are done.
			return ctrl.Result{}, nil
		} else {
			r.Log.Error(err, "failed to create backup job", "jobName", backupJob.Name)
			return ctrl.Result{RequeueAfter: 10 * time.Second}, err
		}
	} else {
		r.Log.Error(err, "failed to get cassandrabackup")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, err
	}
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

func getGrpcServiceAddresses(pods *[]corev1.Pod) ([]string, error) {
	addresses := make([]string, 0)

	for _, pod := range *pods {
		if !hasMedusaSidecar(&pod) {
			return nil, fmt.Errorf("pod %s does not have % sidecar container", pod.Name, backupSidecarName)
		}
		address := fmt.Sprintf("%s:%d", pod.Status.PodIP, backupSidecarPort)
		addresses = append(addresses, address)
	}

	return addresses, nil
}

func hasMedusaSidecar(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == backupSidecarName {
			return true
		}
	}
	return false
}

func newBackupJob(backup *api.CassandraBackup, medusaServices []string) *v1batch.Job {
	return &v1batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backup.Namespace,
			Name:      backup.Name,
		},
		Spec: v1batch.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "medusa-backup-client",
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name: "backup-client",
							Image: "jsanda/backup-client:latest",
							Command: []string{ "/medusa-backup-client" },
							Env: []corev1.EnvVar{
								{
									Name: "BACKUP_NAME",
									Value: backup.Name,
								},
								{
									Name: "BACKUP_NAMESPACE",
									Value: backup.Namespace,
								},
								{
									Name: "MEDUSA_SVC",
									Value: strings.Join(medusaServices, ","),
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *CassandraBackupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&api.CassandraBackup{}).
		Complete(r)
}
