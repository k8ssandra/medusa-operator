package e2e

import (
	"context"
	"fmt"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"strconv"
	"strings"
	"testing"
	"time"

	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	"github.com/k8ssandra/medusa-operator/test/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	timeout3Min = 3 * time.Minute
	timeout7Min = 7 * time.Minute
	timeout15Min = 15 * time.Minute
	retry15Sec = 15 * time.Second
	retry45Sec = 45 * time.Second
)

type user struct {
	Email string
	Name  string
}

func TestBackupAndInPlaceRestore(t *testing.T) {
	namespace := "medusa-dev"
	ctx := context.Background()

	// TODO make this configurable
	s3Bucket := "k8ssandra-medusa-dev"

	if bucketCleaner, err := framework.NewBucketObjectDeleter("aws"); err == nil {
		if _, err := bucketCleaner.DeleteObjects(t, s3Bucket); err != nil {
			t.Logf("failed to delete objects from s3 bucket %s: %s", s3Bucket, err)
		}
	} else {
		t.Logf("failed to create BucketObjectDeleter: %s", err)
	}

	if err := framework.Cleanup(t, namespace, "dc1", retry15Sec, timeout3Min); err != nil {
		t.Fatalf("failed to cleanup before test: %s", err)
	}

	if err := framework.CreateNamespace(namespace); err != nil {
		t.Fatalf("failed to create namespace: %s", err)
	}

	t.Log("running kustomize and kubectl apply")
	if err := framework.KustomizeAndApply(t, namespace, "dev/s3", 3); err != nil {
		t.Fatalf("failed to apply manifests: %s", err)
	}

	storageSecretKey := types.NamespacedName{Namespace: namespace, Name: "medusa-bucket-key"}
	if _, err := framework.GetSecret(t, storageSecretKey); err != nil {
		if apierrors.IsNotFound(err) {
			t.Fatalf("storage secret %s not found", storageSecretKey)
		}  else {
			t.Fatalf("failed to get storage secret %s: %s", storageSecretKey, err)
		}
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

	t.Logf("waiting for cassandradatacenter to become ready")
	if err := framework.WaitForCassandraDatacenterReady(t, key, retry15Sec, timeout7Min); err != nil {
		t.Fatalf("timed out waiting for cassandradatacenter to become ready: %s", err)
	}

	t.Log("creating keyspace")
	if err := createKeyspace(t, namespace); err != nil {
		t.Fatalf("failed to create keyspace: %s", err)
	}

	t.Log("creating table")
	if err := createTable(t, namespace); err != nil {
		t.Fatalf("failed to create table: %s", err)
	}

	t.Log("inserting rows")
	users := []user{
		{Email: "john@test", Name: "John Doe"},
		{Email: "jane@test", Name: "Jane Doe"},
	}
	if err := insertRows(t, namespace, users); err != nil {
		// We use Errorf here because we can still try to test the backup and restore
		// operations. Verification of the restored data will fail as well.
		t.Errorf("failed to insert users: %s", err)
	}

	t.Logf("creating a cassandrabackup")
	backupKey := types.NamespacedName{Namespace: namespace, Name: "test-backup"}
	backup := &api.CassandraBackup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backupKey.Namespace,
			Name:      backupKey.Name,
		},
		Spec: api.CassandraBackupSpec{
			Name:                backupKey.Name,
			CassandraDatacenter: "dc1",
		},
	}

	if err := framework.Client.Create(ctx, backup); err != nil {
		t.Fatalf("failed to create cassandrabackup: %s", err)
	}

	t.Logf("waiting for cassandrabackup %s to finish", backupKey)
	if err := framework.WaitForBackupToFinish(types.NamespacedName{Namespace: namespace, Name: "test-backup"}, retry15Sec, timeout7Min); err != nil {
		t.Fatalf("timed out waiting for backup %s to finish", backupKey)
	}

	t.Logf("checking cassandrabackup status")
	backup, err := framework.GetBackup(backupKey)
	if err != nil {
		t.Fatalf("failed to get backup %s: %s", backupKey, err)
	}
	if len(backup.Status.Failed) > 0 {
		t.Fatalf("the backup operation failed on the following pods: %s", strings.Join(backup.Status.Failed, ","))
	}

	t.Logf("inserting more rows")
	moreUsers := []user{
		{Email: "bob@test", Name: "Bob Smith"},
		{Email: "mary@test", Name: "Mary Smith"},
	}
	if err := insertRows(t, namespace, moreUsers); err != nil {
		t.Errorf("failed to insert more users: %s", err)
	}

	t.Logf("creating a cassandrarestore")
	restoreKey := types.NamespacedName{Namespace: namespace, Name: "test-restore"}
	restore := &api.CassandraRestore{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: restoreKey.Namespace,
			Name:      restoreKey.Name,
		},
		Spec: api.CassandraRestoreSpec{
			Backup:  backupKey.Name,
			InPlace: true,
			CassandraDatacenter: api.CassandraDatacenterConfig{
				Name:        "dc1",
				ClusterName: "medusa-test",
			},
		},
	}

	if err := framework.Client.Create(ctx, restore); err != nil {
		t.Fatalf("failed to create cassandrarestore: %s", err)
	}

	t.Logf("waiting for cassandrarestore %s to finish", restoreKey)
	if err := framework.WaitForRestoreToFinish(restoreKey, retry45Sec, timeout15Min); err != nil {
		t.Fatalf("timed out waiting for restore %s to finish", restoreKey)
	}

	t.Log("checking for restored rows")
	if matches, err := rowCountMatches(t, namespace, len(users)); err == nil {
		if !matches {
			t.Errorf("did not find the expected number of rows")
		}
	} else {
		t.Errorf("failed to check the row count: %s", err)
	}
}

func createKeyspace(t *testing.T, namespace string) error {
	cql := "create keyspace medusa_dev with replication = {'class': 'NetworkTopologyStrategy', 'dc1': 3}"
	pod := "medusa-test-dc1-default-sts-0"

	_, err := framework.ExecCqlsh(t, namespace, pod, cql)
	return err
}

func createTable(t *testing.T, namespace string) error {
	cql := "create table medusa_dev.users (email text primary key, name text)"
	pod := "medusa-test-dc1-default-sts-0"

	_, err := framework.ExecCqlsh(t, namespace, pod, cql)
	return err
}

func insertRows(t *testing.T, namespace string, users []user) error {
	pod := "medusa-test-dc1-default-sts-0"
	for _, user := range users {
		cql := fmt.Sprintf("insert into medusa_dev.users (email, name) values ('%s', '%s')", user.Email, user.Name)
		if _, err := framework.ExecCqlsh(t, namespace, pod, cql); err != nil {
			return err
		}
	}
	return nil
}

func rowCountMatches(t *testing.T, namespace string, count int) (bool, error) {
	pod := "medusa-test-dc1-default-sts-0"
	cql := "select * from medusa_dev.users"

	if output, err := framework.ExecCqlsh(t, namespace, pod, cql); err == nil {
		return strings.Contains(output, strconv.Itoa(count)+" rows"), nil
	} else {
		return false, err
	}
}
