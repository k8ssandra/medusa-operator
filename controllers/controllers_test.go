package controllers

import (
	"context"
	"github.com/bombsimon/logrusr"
	"github.com/go-logr/logr"
	cassdcapi "github.com/k8ssandra/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"testing"
	"time"
)

var cfg *rest.Config
var testClient client.Client
var testEnv *envtest.Environment
var medusaClientFactory *fakeMedusaClientFactory

const (
	TestCassandraDatacenterName = "dc1"
	timeout                     = time.Second * 10
	interval                    = time.Millisecond * 250
)

func TestControllers(t *testing.T) {
	defer afterSuite(t)
	beforeSuite(t)

	ctx := context.Background()
	namespace := "default"

	t.Run("Create Datacenter backup", controllerTest(t, ctx, namespace, testBackupDatacenter))
	t.Run("Restore backup in place", controllerTest(t, ctx, namespace, testInPlaceRestore))
}

func beforeSuite(t *testing.T) {
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "build", "config", "crds")},
	}

	var err error
	cfg, err = testEnv.Start()
	require.NoError(t, err, "failed to start test environment")

	require.NoError(t, registerApis(), "failed to register apis with scheme")

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	require.NoError(t, err, "failed to create controller-runtime manager")

	medusaClientFactory = NewMedusaClientFactory()

	var log logr.Logger
	log = logrusr.NewLogger(logrus.New())
	logf.SetLogger(log)

	err = (&CassandraBackupReconciler{
		Client:        k8sManager.GetClient(),
		Log:           log.WithName("controllers").WithName("CassandraBackup"),
		Scheme:        scheme.Scheme,
		ClientFactory: medusaClientFactory,
	}).SetupWithManager(k8sManager)
	require.NoError(t, err, "failed to set up CassandraBackupReconciler")

	err = (&CassandraRestoreReconciler{
		Client: k8sManager.GetClient(),
		Log:    log.WithName("controllers").WithName("CassandraRestore"),
		Scheme: scheme.Scheme,
	}).SetupWithManager(k8sManager)
	require.NoError(t, err, "failed to set up CassandraRestoreReconciler")

	go func() {
		err = k8sManager.Start(ctrl.SetupSignalHandler())
		assert.NoError(t, err, "failed to start manager")
	}()

	testClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err, "failed to create controller-runtime client")
}

func registerApis() error {
	if err := api.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	if err := cassdcapi.AddToScheme(scheme.Scheme); err != nil {
		return err
	}

	return nil
}

func afterSuite(t *testing.T) {
	if testEnv != nil {
		err := testEnv.Stop()
		assert.NoError(t, err, "failed to stop test environment")
	}
}

type ControllerTest func(*testing.T, context.Context, string)

func controllerTest(t *testing.T, ctx context.Context, namespace string, test ControllerTest) func(*testing.T) {
	// TODO Remove namespace arg and make it random

	defer deleteNamespace(t, ctx, namespace)

	//err := testClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
	//require.NoError(t, err, "failed to create namespace")

	return func(t *testing.T) {
		test(t, ctx, namespace)
	}
}

func deleteNamespace(t *testing.T, ctx context.Context, namespace string) {
	//err := testClient.Delete(ctx, )
}
