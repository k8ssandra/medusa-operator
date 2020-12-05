package controllers

import (
	"context"
	"fmt"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
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
			},
		}
		Expect(k8sClient.Create(context.Background(), cassdc)).Should(Succeed())
		Eventually(func() bool {
			created := &cassdcapi.CassandraDatacenter{}
			err := k8sClient.Get(context.Background(), cassdcKey, created)
			return err == nil
		}, timeout, interval).Should(BeTrue())

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
		Eventually(func() bool {
			created := &corev1.Service{}
			err := k8sClient.Get(context.Background(), dcServiceKey, created)
			return err == nil
		}, timeout, interval).Should(BeTrue())

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
		Eventually(func() bool {
			created := &api.CassandraBackup{}
			err := k8sClient.Get(context.Background(), backupKey, created)
			return err == nil
		}, timeout, interval).Should(BeTrue())

		By("check that the backups are started")
		Eventually(func() bool {
			updated := &api.CassandraBackup{}
			err := k8sClient.Get(context.Background(), backupKey, updated)
			if err != nil {
				return false
			}
			return !updated.Status.StartTime.IsZero()
		}, timeout, interval).Should(BeTrue())
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
	}
}
