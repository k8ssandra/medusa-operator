package framework

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	cassdcv1beta1 "github.com/datastax/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"gopkg.in/yaml.v3"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	OperatorRetryInterval = 5 * time.Second
	OperatorTimeout       = 30 * time.Second
	defaultOverlay        = "k8ssandra"
)

var (
	Client client.Client
)

func init() {
	Client = createClient()
}

func createClient() client.Client {
	if err := api.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to register medusa-operator GVK with scheme: %s", err)
	}

	if err := cassdcv1beta1.AddToScheme(scheme.Scheme); err != nil {
		log.Fatalf("failed to register cass-operator GVK with scheme: %s", err)
	}

	configLoadingRules, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		log.Fatalf("failed to create ClientConfigLoadingRules: %s", err)
	}

	config := clientcmd.NewDefaultClientConfig(*configLoadingRules, &clientcmd.ConfigOverrides{})
	cfg, err := config.ClientConfig()
	if err != nil {
		log.Fatalf("failed to create ClientConfig: %s", err)
	}

	cl, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		log.Fatalf("failed to create controller-runtime client: %s", err)
	}

	return cl
}

// Runs kustomize build followed kubectl apply. dir specifies the name of a test directory.
// By default this function will run kustomize build on dir/overlays/k8ssandra. This will
// result in using upstream operator images. If you are testing against a fork, then set
// the TEST_OVERLAY environment variable to specify the fork overlay to use. When
// TEST_OVERLAY is set this function will run kustomize build on
// dir/overlays/forks/TEST_OVERLAY which will allow you to use a custom operator image.
func KustomizeAndApply(t *testing.T, namespace, dir string) error {
	kustomizeDir := ""

	path, err := os.Getwd()
	if err != nil {
		return err
	}

	if overlay, found := os.LookupEnv("TEST_OVERLAY"); found {
		kustomizeDir = overlay
	} else {
		kustomizeDir = filepath.Clean(path + "/../config/" + dir)
	}

	kustomize := exec.Command("kustomize", "build", kustomizeDir)
	var stdout, stderr bytes.Buffer
	kustomize.Stdout = &stdout
	kustomize.Stderr = &stderr
	err = kustomize.Run()

	if err != nil {
		return err
	}

	kubectl := exec.Command("kubectl", "-n", namespace, "apply", "-f", "-")
	kubectl.Stdin = &stdout
	out, err := kubectl.CombinedOutput()
	t.Log(string(out))

	return err
}

// Blocks until the cass-operator Deployment is ready. This function assumes that there will be a
// single replica in the Deployment.
func WaitForCassOperatorReady(namespace string) error {
	key := types.NamespacedName{Namespace: namespace, Name: "cass-operator"}
	return WaitForDeploymentReady(key, 1, OperatorRetryInterval, OperatorTimeout)
}

func WaitForMedusaOperatorReady(namespace string) error {
	key := types.NamespacedName{Namespace: namespace, Name: "medusa-operator"}
	return WaitForDeploymentReady(key, 1, OperatorRetryInterval, OperatorTimeout)
}

// Blocks until .Status.ReadyReplicas == readyReplicas or until timeout is reached. An error is returned
// if fetching the Deployment fails.
func WaitForDeploymentReady(key types.NamespacedName, readyReplicas int32, retryInterval, timeout time.Duration) error {
	return wait.Poll(retryInterval, timeout, func() (bool, error) {
		deployment := &appsv1.Deployment{}
		err := Client.Get(context.Background(), key, deployment)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return true, err
		}
		return deployment.Status.ReadyReplicas == readyReplicas, nil
	})
}

// Blocks until the CassandraDatacenter is ready or until the timeout is reached. Readiness
// is determined by the Ready condition in the CassandraDatacenter status. An error is
// returned if fetching the CassandraDatacenter fails.
func WaitForCassandraDatacenterReady(t *testing.T, key types.NamespacedName, retryInterval, timeout time.Duration) error {
	start := time.Now()
	return wait.Poll(retryInterval, timeout, func() (bool, error) {
		cassdc, err := GetCassandraDatacenter(key)
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("cassandradatacenter %s not found", key)
				return false, nil
			}
			t.Logf("failed to get cassandradatacenter %s: %s", key, err)
			return true, err
		}
		logCassDcStatus(t, cassdc, start)
		status := cassdc.GetConditionStatus(cassdcv1beta1.DatacenterReady)
		return status == corev1.ConditionTrue, nil
	})
}

func GetCassandraDatacenter(key types.NamespacedName) (*cassdcv1beta1.CassandraDatacenter, error) {
	cassdc := &cassdcv1beta1.CassandraDatacenter{}
	err := Client.Get(context.Background(), key, cassdc)

	return cassdc, err
}

func logCassDcStatus(t *testing.T, cassdc *cassdcv1beta1.CassandraDatacenter, start time.Time) {
	if d, err := yaml.Marshal(cassdc.Status); err == nil {
		duration := time.Now().Sub(start)
		sec := int(duration.Seconds())
		t.Logf("cassandradatacenter status after %d sec...\n%s\n\n", sec, string(d))
	} else {
		t.Logf("failed to log cassandradatacenter status: %s", err)
	}
}

func WaitForBackupToFinish(key types.NamespacedName, retryInterval, timeout time.Duration) error {
	return wait.Poll(retryInterval, timeout, func() (bool, error) {
		backup := &api.CassandraBackup{}
		err := Client.Get(context.Background(), key, backup)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return true, err
		}
		return !backup.Status.FinishTime.IsZero(), nil
	})
}

func GetBackup(key types.NamespacedName) (*api.CassandraBackup, error) {
	backup := &api.CassandraBackup{}
	err := Client.Get(context.Background(), key, backup)

	return backup, err
}