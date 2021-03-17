package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	"github.com/k8ssandra/medusa-operator/test/framework"
	"k8s.io/apimachinery/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

func TestExec(t *testing.T) {
	namespace := "medusa-dev"
	ctx := context.Background()

	t.Log("running kustomize and kubectl apply")
	if err := framework.KustomizeAndApply(t, namespace, "dev/s3"); err != nil {
		t.Fatalf("failed to apply manifests: %s", err)
	}

	t.Log("waiting for cass-operator to become ready")
	if err := framework.WaitForCassOperatorReady(namespace); err != nil {
		t.Fatalf("timed out waiting for cass-operator to become ready: %s", err)
	}

	t.Log("waiting for medusa-operator to become ready")
	if err := framework.WaitForMedusaOperatorReady(namespace); err != nil {
		t.Fatalf("timed out waiting for medusa-operator to become ready: %s", err)
	}

	key := types.NamespacedName{Namespace: namespace, Name: "dc1"}
	cassdcRetryInterval := 15 * time.Second
	cassdcTimeout := 7 * time.Minute

	t.Logf("waiting for cassandradatacenter to become ready")
	if err := framework.WaitForCassandraDatacenterReady(t, key, cassdcRetryInterval, cassdcTimeout); err != nil {
		t.Fatalf("timed out waiting for cassandradatacenter to become ready: %s", err)
	}

	dc, err := framework.GetCassandraDatacenter(key)
	if err != nil {
		t.Fatalf("failed to get cassandradatacenter %s: %s", key, err)
	}

	if dc == nil {
		t.FailNow()
	}

	backupKey := types.NamespacedName{Namespace: namespace, Name: "test-backup"}

	backup := &api.CassandraBackup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backupKey.Namespace,
			Name: backupKey.Name,
		},
		Spec: api.CassandraBackupSpec{
			Name: backupKey.Name,
			CassandraDatacenter: "dc1",
		},
	}

	t.Logf("creating cassandrabackup ")
	if err := framework.Client.Create(ctx, backup); err != nil {
		t.Fatal("failed to create backup")
	}

	t.Logf("waiting for cassandrabackup %s to finish", backupKey)
	if err := framework.WaitForBackupToFinish(types.NamespacedName{Namespace: namespace, Name: "test-backup"}, cassdcRetryInterval, cassdcTimeout); err != nil {
		t.Fatalf("timed out waiting for backup %s to finish", backupKey)
	}

	backup, err = framework.GetBackup(backupKey)
	if err != nil {
		t.Fatalf("failed to get backup %s: %s", backupKey, err)
	}
	if len(backup.Status.Failed) > 0 {
		t.Fatalf("the backup operation failed on the following pods: %s", strings.Join(backup.Status.Failed, ","))
	}
}
