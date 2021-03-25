package controllers

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/k8ssandra/medusa-operator/pkg/medusa"
	"github.com/k8ssandra/medusa-operator/pkg/pb"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cassdcapi "github.com/datastax/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
)

const (
	TestCassandraDatacenterName = "dc1"
	timeout                     = time.Second * 10
	interval                    = time.Millisecond * 250
)

var _ = Describe("CassandraBackup controller", func() {
	i := 0
	testNamespace := ""

	BeforeEach(func() {
		testNamespace = "backup-test-" + strconv.Itoa(i)
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(context.Background(), testNamespace)).Should(Succeed())
		i = i + 1
	})

	Specify("create a new backup for an existing CassandraDatacenter", func() {
		By("create a CassandraDatacenter")
		backupName := "test-backup"

		cassdcKey := types.NamespacedName{
			Name:      TestCassandraDatacenterName,
			Namespace: testNamespace,
		}

		cassdc := &cassdcapi.CassandraDatacenter{
			ObjectMeta: metav1.ObjectMeta{
				Name:        cassdcKey.Name,
				Namespace:   cassdcKey.Namespace,
				Annotations: map[string]string{},
			},
			Spec: cassdcapi.CassandraDatacenterSpec{
				ClusterName:   "test-dc",
				ServerType:    "cassandra",
				ServerVersion: "3.11.7",
				Size:          3,
				StorageConfig: cassdcapi.StorageConfig{
					CassandraDataVolumeClaimSpec: &corev1.PersistentVolumeClaimSpec{
						VolumeName: "data",
					},
				},
				PodTemplateSpec: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{},
						InitContainers: []corev1.Container{
							{
								Name: "medusa-restore",
								Env: []corev1.EnvVar{
									{
										Name:  "MEDUSA_MODE",
										Value: "RESTORE",
									},
								},
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), cassdc)).Should(Succeed())
		Eventually(func() error {
			created := &cassdcapi.CassandraDatacenter{}
			return k8sClient.Get(context.Background(), cassdcKey, created)
		}, timeout, interval).Should(Succeed())

		By("create the datacenter service")
		dcServiceKey := types.NamespacedName{Namespace: cassdcKey.Namespace, Name: cassdc.GetAllPodsServiceName()}
		dcService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: dcServiceKey.Namespace,
				Name:      dcServiceKey.Name,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{
					cassdcapi.ClusterLabel: cassdc.Spec.ClusterName,
				},
				Ports: []corev1.ServicePort{
					{
						Name: "cql",
						Port: 9042,
					},
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), dcService)).Should(Succeed())
		Eventually(func() error {
			created := &corev1.Service{}
			return k8sClient.Get(context.Background(), dcServiceKey, created)
		}, timeout, interval).Should(Succeed())

		By("create the CassandraDatacenter pods")
		createCassandraDatacenterPods(cassdc)

		By("make the CassandraDatacenter ready")
		patch := client.MergeFrom(cassdc.DeepCopy())
		cassdc.Status.CassandraOperatorProgress = cassdcapi.ProgressReady
		cassdc.Status.Conditions = []cassdcapi.DatacenterCondition{
			{
				Status: corev1.ConditionTrue,
				Type:   cassdcapi.DatacenterReady,
			},
		}
		Expect(k8sClient.Status().Patch(context.Background(), cassdc, patch)).Should(Succeed())
		Eventually(func() bool {
			updated := &cassdcapi.CassandraDatacenter{}
			err := k8sClient.Get(context.Background(), cassdcKey, updated)
			if err != nil {
				return false
			}
			return cassdc.Status.CassandraOperatorProgress == cassdcapi.ProgressReady &&
				updated.GetConditionStatus(cassdcapi.DatacenterReady) == corev1.ConditionTrue
		}, timeout, interval).Should(BeTrue())

		By("create a CassandraBackup")
		backupKey := types.NamespacedName{Namespace: testNamespace, Name: backupName}
		backup := &api.CassandraBackup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      backupName,
			},
			Spec: api.CassandraBackupSpec{
				Name:                backupName,
				CassandraDatacenter: cassdcKey.Name,
			},
		}
		Expect(k8sClient.Create(context.Background(), backup)).Should(Succeed())
		Eventually(func() error {
			created := &api.CassandraBackup{}
			return k8sClient.Get(context.Background(), backupKey, created)
		}, timeout, interval).Should(Succeed())

		By("verify that the backups are started")
		Eventually(func() bool {
			updated := &api.CassandraBackup{}
			err := k8sClient.Get(context.Background(), backupKey, updated)
			if err != nil {
				return false
			}
			return !updated.Status.StartTime.IsZero()
		}, timeout, interval).Should(BeTrue())

		By("verify that the CassandraDatacenter spec is added to the backup status")
		Eventually(func() bool {
			updated := &api.CassandraBackup{}
			err := k8sClient.Get(context.Background(), backupKey, updated)
			if err != nil {
				return false
			}

			return updated.Status.CassdcTemplateSpec.Spec.ClusterName == cassdc.Spec.ClusterName &&
				updated.Status.CassdcTemplateSpec.Spec.Size == cassdc.Spec.Size &&
				updated.Status.CassdcTemplateSpec.Spec.ServerVersion == cassdc.Spec.ServerVersion &&
				updated.Status.CassdcTemplateSpec.Spec.ServerImage == cassdc.Spec.ServerImage &&
				reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.Config, cassdc.Spec.Config) &&
				updated.Status.CassdcTemplateSpec.Spec.ManagementApiAuth == cassdc.Spec.ManagementApiAuth &&
				reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.Resources, cassdc.Spec.Resources) &&
				reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.SystemLoggerResources, cassdc.Spec.SystemLoggerResources) &&
				reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.ConfigBuilderResources, cassdc.Spec.ConfigBuilderResources) &&
				reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.Racks, cassdc.Spec.Racks) &&
				reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.StorageConfig, cassdc.Spec.StorageConfig) &&
				updated.Status.CassdcTemplateSpec.Spec.Stopped == cassdc.Spec.Stopped &&
				updated.Status.CassdcTemplateSpec.Spec.ConfigBuilderImage == cassdc.Spec.ConfigBuilderImage &&
				updated.Status.CassdcTemplateSpec.Spec.AllowMultipleNodesPerWorker == cassdc.Spec.AllowMultipleNodesPerWorker &&
				updated.Status.CassdcTemplateSpec.Spec.ServiceAccount == cassdc.Spec.ServiceAccount &&
				reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.NodeSelector, cassdc.Spec.NodeSelector) &&
				// reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.PodTemplateSpec, cassdc.Spec.PodTemplateSpec) &&
				reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.Users, cassdc.Spec.Users) &&
				reflect.DeepEqual(updated.Status.CassdcTemplateSpec.Spec.AdditionalSeeds, cassdc.Spec.AdditionalSeeds)
		}, timeout, interval).Should(BeTrue())

		By("verify the backup finished")
		Eventually(func() bool {
			updated := &api.CassandraBackup{}
			err := k8sClient.Get(context.Background(), backupKey, updated)
			if err != nil {
				return false
			}
			return len(updated.Status.Finished) > 0
		}, timeout, interval).Should(BeTrue())

		By("verify that medusa gRPC clients are invoked")
		Expect(medusaClientFactory.GetRequestedBackups()).To(Equal(map[string][]string{
			fmt.Sprintf("%s:%d", getPodIpAddress(0), backupSidecarPort): {backupName},
			fmt.Sprintf("%s:%d", getPodIpAddress(1), backupSidecarPort): {backupName},
			fmt.Sprintf("%s:%d", getPodIpAddress(2), backupSidecarPort): {backupName},
		}))

		By("restoring to a new cluster")
		restoreKey := types.NamespacedName{Namespace: testNamespace, Name: backupName + "restore"}
		restore := &api.CassandraRestore{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: restoreKey.Namespace,
				Name:      restoreKey.Name,
			},
			Spec: api.CassandraRestoreSpec{
				Backup: backup.Name,
				CassandraDatacenter: api.CassandraDatacenterConfig{
					Name:        "dc1-restored",
					ClusterName: "k8ssandra",
				},
			},
		}

		Expect(k8sClient.Create(context.TODO(), restore)).To(Succeed())
		Eventually(func() error {
			created := &api.CassandraRestore{}
			return k8sClient.Get(context.Background(), restoreKey, created)
		}, timeout, interval).Should(Succeed())

		By("verifying a copy of the cluster was created")
		Eventually(func() bool {
			createdCassDc := &cassdcapi.CassandraDatacenter{}
			cassDcRestoredKey := types.NamespacedName{
				Name:      "dc1-restored",
				Namespace: restoreKey.Namespace,
			}

			if err := k8sClient.Get(context.Background(), cassDcRestoredKey, createdCassDc); err != nil {
				return false
			}

			return true
		}, timeout, interval).Should(BeTrue())

		By("deleting the backup")
		Expect(k8sClient.Delete(context.Background(), backup)).Should(Succeed())
		Eventually(func() bool {
			existing := &api.CassandraBackup{}
			err := k8sClient.Get(context.Background(), backupKey, existing)
			return errors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())

		By("verifying a medusa gRPC client is invoked")
		Expect(len(medusaClientFactory.GetDeletedBackups())).To(Equal(1))
	})
})

func createCassandraDatacenterPods(cassdc *cassdcapi.CassandraDatacenter) {
	for i := int32(0); i < cassdc.Spec.Size; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: cassdc.Namespace,
				Name:      fmt.Sprintf("%s-%d", cassdc.Spec.ClusterName, i),
				Labels: map[string]string{
					cassdcapi.ClusterLabel: cassdc.Spec.ClusterName,
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "cassandra",
						Image: "cassandra",
					},
					{
						Name:  backupSidecarName,
						Image: backupSidecarName,
					},
				},
			},
		}
		Expect(k8sClient.Create(context.Background(), pod)).Should(Succeed())
		Eventually(func() bool {
			created := &corev1.Pod{}
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, created)
			return err == nil
		}, timeout, interval).Should(BeTrue())

		patch := client.MergeFrom(pod.DeepCopy())
		pod.Status.PodIP = getPodIpAddress(int(i))
		Expect(k8sClient.Status().Patch(context.Background(), pod, patch)).Should(Succeed())
		Eventually(func() bool {
			updated := &corev1.Pod{}
			err := k8sClient.Get(context.Background(), types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, updated)
			if err != nil {
				return false
			}
			return len(updated.Status.PodIP) > 0
		}, timeout, interval).Should(BeTrue())
	}
}

// Creates a fake ip address with the pod's orginal index from the StatefulSet
func getPodIpAddress(index int) string {
	return "192.168.1." + strconv.Itoa(50+index)
}

type fakeMedusaClientFactory struct {
	clientsMutex sync.Mutex
	clients      map[string]*fakeMedusaClient
}

func NewMedusaClientFactory() *fakeMedusaClientFactory {
	return &fakeMedusaClientFactory{clients: make(map[string]*fakeMedusaClient, 0)}
}

func (f *fakeMedusaClientFactory) NewClient(address string) (medusa.Client, error) {
	medusaClient := newFakeMedusaClient()
	f.clientsMutex.Lock()
	f.clients[address] = medusaClient
	f.clientsMutex.Unlock()
	return medusaClient, nil
}

func (f *fakeMedusaClientFactory) GetRequestedBackups() map[string][]string {
	requestedBackups := make(map[string][]string)
	for k, v := range f.clients {
		requestedBackups[k] = v.RequestedBackups
	}
	return requestedBackups
}

func (f *fakeMedusaClientFactory) GetDeletedBackups() []string {
	deletedBackups := make([]string, 0)
	for _, v := range f.clients {
		deletedBackups = append(deletedBackups, v.DeletedBackups...)
	}
	return deletedBackups
}

type fakeMedusaClient struct {
	RequestedBackups []string
	DeletedBackups   []string
}

func newFakeMedusaClient() *fakeMedusaClient {
	return &fakeMedusaClient{
		RequestedBackups: make([]string, 0),
		DeletedBackups:   make([]string, 0),
	}
}

func (c *fakeMedusaClient) Close() error {
	return nil
}

func (c *fakeMedusaClient) CreateBackup(ctx context.Context, name string) error {
	c.RequestedBackups = append(c.RequestedBackups, name)
	return nil
}

func (c *fakeMedusaClient) GetBackups(ctx context.Context) ([]*pb.BackupSummary, error) {
	return nil, nil
}

func (c *fakeMedusaClient) DeleteBackup(ctx context.Context, name string) error {
	c.DeletedBackups = append(c.DeletedBackups, name)
	return nil
}
