package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	"k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	MedusaSvcEnvVar      = "MEDUSA_SVC"
	BackupNameEnvVar     = "BACKUP_NAME"
	BackupNamespaceEnvar = "BACKUP_NAMESPACE"
)

var (
	logger = ctrl.Log.WithName("backup-client")

	mgr manager.Manager
)

// +kubebuilder:rbac:groups=cassandra.k8ssandra.io,namespace="medusa-operator",resources=cassandrabackups,verbs=get;list;watch
// +kubebuilder:rbac:groups=cassandra.k8ssandra.io,namespace="medusa-operator",resources=cassandrabackups/status,verbs=get;update;patch

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	ctx := context.Background()

	logger.Info("starting")

	backupName := getEnvVar(BackupNameEnvVar)
	backupNamespace := getEnvVar(BackupNamespaceEnvar)
	medusaSvc := getEnvVar(MedusaSvcEnvVar)
	medusaServices := strings.Split(medusaSvc, ",")

	logger.Info("calling medusa gRPC services", "MedusaServices", medusaServices)

	_, err := createK8sClient(backupNamespace)
	if err != nil {
		fmt.Printf("failed to create k8s client: %s", err)
		logger.Error(err, "failed to create k8s api client")
		os.Exit(1)
	}

	logger.Info("starting backups")
	var wg = sync.WaitGroup{}
	for _, svc := range medusaServices {
		logger.Info("submitting request")
		go func(addr string) {
			wg.Add(1)
			if err := doBackup(ctx, backupName, addr); err == nil {
				logger.Info("finished backup", "Backup", backupName, "Address", addr)
			} else {
				logger.Error(err, "backup failed", "Backup", backupName, "Address", addr)
			}
			wg.Done()
		}(svc)
	}
	wg.Wait()
	logger.Info("backups finished")
}

func getEnvVar(name string) string {
	value := os.Getenv(name)
	if len(value) == 0 {
		log.Fatalf("the %s environment variable is required", name)
	}
	return value
}

func createK8sClient(namespace string) (client.Client, error) {
	err := api.AddToScheme(scheme.Scheme)
	if err != nil {
		return nil, err
	}

	mgr, err = ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0", // no metrics... for now
		Port:               9443,
		LeaderElection:     false,
		Namespace:          namespace, // namespaced-scope when the value is not an empty string
	})
	if err != nil {
		return nil, err
	}

	return mgr.GetClient(), nil
}

func doBackup(ctx context.Context, name, addr string) error {
	//
	return nil
}
