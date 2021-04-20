package e2e

import (
	"context"
	"flag"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"strings"
	"testing"
	"time"

	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	"github.com/k8ssandra/medusa-operator/test/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	timeout3Min  = 3 * time.Minute
	timeout7Min  = 7 * time.Minute
	timeout15Min = 15 * time.Minute
	retry15Sec   = 15 * time.Second
	retry45Sec   = 45 * time.Second

	datacenterName = "dc1"
)

type user struct {
	Email string
	Name  string
}

var (
	cleanupBeforeTestGroup bool
	cleanupAfterTestGroup  bool
	truncateAfterTestGroup bool
	runKustomize           bool
)

func init() {
	flag.BoolVar(&cleanupBeforeTestGroup, "cleanup.beforeTestGroup", true,
		"Deletes all objects in the namespace as well as the namespace before the test group is executed")
	flag.BoolVar(&cleanupAfterTestGroup, "cleanup.afterTestGroup", true,
		"Deletes all objects in the namespace as well as the namespace after the test group is executed")
	flag.BoolVar(&truncateAfterTestGroup, "truncate.afterTestGroup", false,
		"Truncates the table in Cassandra after the test group is executed. Ignored if cleanup.afterTestGroup is true.")
	flag.BoolVar(&runKustomize, "runKustomize", true,
		"Run kustomize build and apply generated manifests.")
}

func TestBackupInPlaceRestore(t *testing.T) {
	storageTypes := []string{"s3", "gcs"}

	for _, storageType := range storageTypes {
		t.Run(testName(storageType, "TestBase"), newTestBase(storageType))
	}
}

func newTestBase(storageType string) func(*testing.T) {
	return func(t *testing.T) {
		namespace := "medusa-test"

		cassandra := framework.NewCassandraSchemaManager(t, namespace)

		if cleanupBeforeTestGroup {
			if err := framework.Cleanup(t, namespace, datacenterName, retry15Sec, timeout3Min); err != nil {
				t.Fatalf("failed to cleanup before test: %s", err)
			}
		}

		prepareNamespace(t, storageType, namespace)
		prepareSchema(t, cassandra)

		if cleanupAfterTestGroup {
			defer framework.Cleanup(t, namespace, datacenterName, retry15Sec, timeout3Min)
		}

		t.Run(testName(storageType, "BackUpInPlaceRestoreWithShutdown"), newBackupRestoreTest(storageType, namespace, cassandra, doBackupAndInPlaceRestore))

		t.Run(testName(storageType, "BackupInPlaceRestoreWithoutShutdown"), noOpTest())

		t.Run(testName(storageType, "BackupInPlaceRestoreAfterScaleUp"), noOpTest())
	}
}

func testName(storageType, name string) string {
	return "[" + storageType + "]" + name
}

type baseTest func(*testing.T)

type backupRestoreTest func(t *testing.T, namespace string, cassandra *framework.CassandraSchemaManager)

func newBackupRestoreTest(storageType, namespace string, cassandra *framework.CassandraSchemaManager, test backupRestoreTest) func(*testing.T) {
	return func(t *testing.T) {
		defer framework.DumpClusterInfo(t, storageType, namespace)
		test(t, namespace, cassandra)
	}
}

func noOpTest() baseTest {
	return func(t *testing.T) {}
}

func prepareNamespace(t *testing.T, storageType, namespace string) {
	overlayDir, err := framework.GetKustomizeOverlayDir(storageType)
	if err != nil {
		t.Fatalf("failed to get kustomize overlay directory: %s", err)
	}

	defer framework.DumpClusterInfoOnFailure(t, namespace, storageType)

	if err := framework.CreateNamespace(namespace); err != nil {
		t.Fatalf("failed to create namespace: %s", err)
	}

	if runKustomize {
		t.Log("running kustomize and kubectl apply")
		if err := framework.KustomizeAndApply(t, namespace, overlayDir, 3); err != nil {
			t.Fatalf("failed to apply manifests: %s", err)
		}
	}

	storageSecretKey := types.NamespacedName{Namespace: namespace, Name: "medusa-bucket-key"}
	if _, err := framework.GetSecret(t, storageSecretKey); err != nil {
		if apierrors.IsNotFound(err) {
			t.Fatalf("storage secret %s not found", storageSecretKey)
		} else {
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

	key := types.NamespacedName{Namespace: namespace, Name: datacenterName}

	t.Logf("waiting for cassandradatacenter to become ready")
	if err := framework.WaitForCassandraDatacenterReady(t, key, retry15Sec, timeout7Min); err != nil {
		t.Fatalf("timed out waiting for cassandradatacenter to become ready: %s", err)
	}
}

func prepareSchema(t *testing.T, cassandra *framework.CassandraSchemaManager) {
	if err := cassandra.CreateKeyspace(); err != nil {
		t.Fatalf("failed to create keyspace: %s", err)
	}

	if err := cassandra.CreateUsersTable(); err != nil {
		t.Fatalf("failed to create table: %s", err)
	}
}

func doBackupAndInPlaceRestore(t *testing.T, namespace string, cassandra *framework.CassandraSchemaManager) {
	ctx := context.Background()

	users := []framework.User{
		{Email: "john@test", Name: "John Doe"},
		{Email: "jane@test", Name: "Jane Doe"},
	}
	if err := cassandra.InsertRows(users); err != nil {
		t.Fatalf("failed to insert users: %s", err)
	}

	backupKey := types.NamespacedName{Namespace: namespace, Name: "backup-" + randomSuffix(8)}

	t.Logf("creating a cassandrabackup %s", backupKey.Name)

	backup := &api.CassandraBackup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: backupKey.Namespace,
			Name:      backupKey.Name,
		},
		Spec: api.CassandraBackupSpec{
			Name:                backupKey.Name,
			CassandraDatacenter: datacenterName,
		},
	}

	if err := framework.Client.Create(ctx, backup); err != nil {
		t.Fatalf("failed to create cassandrabackup: %s", err)
	}

	t.Logf("waiting for cassandrabackup %s to finish", backupKey)
	if err := framework.WaitForBackupToFinish(backupKey, retry15Sec, timeout7Min); err != nil {
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
	moreUsers := []framework.User{
		{Email: "bob@test", Name: "Bob Smith"},
		{Email: "mary@test", Name: "Mary Smith"},
	}
	if err := cassandra.InsertRows(moreUsers); err != nil {
		t.Errorf("failed to insert more users: %s", err)
	}

	t.Logf("creating a cassandrarestore")
	restoreKey := types.NamespacedName{Namespace: namespace, Name: "restore-" + backupKey.Name}
	restore := &api.CassandraRestore{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: restoreKey.Namespace,
			Name:      restoreKey.Name,
		},
		Spec: api.CassandraRestoreSpec{
			Backup:  backupKey.Name,
			InPlace: true,
			Shutdown: true,
			CassandraDatacenter: api.CassandraDatacenterConfig{
				Name:        datacenterName,
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
	if matches, err := cassandra.RowCountMatches(len(users)); err == nil {
		if !matches {
			t.Errorf("did not find the expected number of rows")
		}
	} else {
		t.Errorf("failed to check the row count: %s", err)
	}
}

func randomSuffix(length int) string {
	return rand.String(length)
}
