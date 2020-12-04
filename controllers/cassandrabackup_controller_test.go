package controllers

import (
	"context"
	"strconv"

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
)

var _ = Describe("Reaper controller", func() {
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
			},
		}
		Expect(k8sClient.Create(context.Background(), cassdc)).Should(Succeed())

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

		updatedDc := &cassdcapi.CassandraDatacenter{}
		Expect(k8sClient.Get(context.Background(), cassdcKey, updatedDc)).To(Succeed())
		Expect(updatedDc.Status.CassandraOperatorProgress).To(Equal(cassdcapi.ProgressReady))

		By("create a CassandraBackup")
		backup := &api.CassandraBackup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: testNamespace,
				Name:      backupName,
			},
			Spec: api.CassandraBackupSpec{
				Name:                backupName,
				CassandraDatacenter: cassdcKey.String(),
			},
		}
		Expect(k8sClient.Create(context.Background(), backup)).Should(Succeed())
	})
})
