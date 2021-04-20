package framework

import (
	"bytes"
	"context"
	"fmt"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/shell"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cassdcv1beta1 "github.com/datastax/cass-operator/operator/pkg/apis/cassandra/v1beta1"
	"github.com/gruntwork-io/terratest/modules/k8s"
	api "github.com/k8ssandra/medusa-operator/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func GetKustomizeOverlayDir(storageType string) (string, error) {
	var overlayDir string

	path, err := os.Getwd()
	if err != nil {
		return "", err
	}

	if overlay, found := os.LookupEnv("TEST_OVERLAY"); found {
		overlayDir = filepath.Clean(overlay)
	} else {
		overlayDir = filepath.Clean(path + "/../config/dev/")
	}

	return filepath.Join(overlayDir, storageType), nil
}

// Runs kustomize build followed kubectl apply. dir specifies the name of a test directory.
// By default this function will run kustomize build on dir/overlays/k8ssandra. This will
// result in using upstream operator images. If you are testing against a fork, then set
// the TEST_OVERLAY environment variable to specify the fork overlay to use. When
// TEST_OVERLAY is set this function will run kustomize build on
// dir/overlays/forks/TEST_OVERLAY which will allow you to use a custom operator image.
func KustomizeAndApply(t *testing.T, namespace, kustomizeDir string, retries int) error {
	var err error

	for i := 0; i < retries; i++ {
		kustomize := exec.Command("kustomize", "build", kustomizeDir)
		var stdout, stderr bytes.Buffer
		kustomize.Stdout = &stdout
		kustomize.Stderr = &stderr
		err = kustomize.Run()

		if err != nil {
			t.Log(kustomize.Stderr)
			t.Logf("kustomize build failed: %s", err)
			continue
		}

		kubectl := exec.Command("kubectl", "-n", namespace, "apply", "-f", "-")
		kubectl.Stdin = &stdout
		out, err := kubectl.CombinedOutput()
		t.Log(string(out))

		if err == nil {
			return nil
		} else {
			t.Logf("kubectl apply failed: %s", err)
		}
	}

	return err
}

func DumpClusterInfoOnFailure(t *testing.T, namespace, storageType string) error {
	if t.Failed() {
		return DumpClusterInfo(t, namespace, storageType)
	}
	return nil
}

func DumpClusterInfo(t *testing.T, storageType, namespace string) error {
	if err := DumpClusterInfoE(t, storageType, namespace); err != nil {
		t.Logf("failed to dump cluster info: %s", err)
		return err
	}
	return nil
}

func DumpClusterInfoE(t *testing.T, storageType, namespace string) error {
	t.Logf("dumping cluster info")

	segments := strings.Split(t.Name(), "/")
	segment := segments[len(segments) - 1]
	tag := "[" + storageType + "]"
	testName := segment[len(tag):]

	outputDir := "../../build/test/" + storageType + "/" + testName
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create test output directory %s: %s", outputDir, err)
	}

	cmd := shell.Command{
		Command: "kubectl",
		Args: []string{
			"cluster-info",
			"dump",
			"--namespaces",
			namespace,
			"-o",
			"yaml",
			"--output-directory",
			outputDir,
		},
		Logger: logger.Discard,
	}
	_, err := shell.RunCommandAndGetOutputE(t, cmd)
	return err
}

// Deletes the CassandraDataenter and then the namespace. Both deletions are blocking which
// means when this function returns both the CassandraDatacenter and namespace will have
// been terminated. retryInterval and timeout are applied to both deletion operations.
func Cleanup(t *testing.T, namespace, dc string, retryInterval, timeout time.Duration) error {
	if err := DeleteCassandraDatacenterSync(t, types.NamespacedName{Namespace: namespace, Name: dc}, retryInterval, timeout); err != nil {
		return err
	}

	return DeleteNamespaceSync(t, namespace, retryInterval, timeout)
}

func ExecCqlsh(t *testing.T, namespace, pod, query string) (string, error) {
	options := k8s.NewKubectlOptions("", "", namespace)

	if output, err := k8s.RunKubectlAndGetOutputE(t, options, "exec", "-i", pod, "-c", "cassandra", "--", "cqlsh", "-e", query); err == nil {
		//t.Log(output)
		return output, nil
	} else {
		return output, err
	}

}

func GetSecret(t *testing.T, key types.NamespacedName) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	err := Client.Get(context.Background(), key, secret)
	return secret, err
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
	return wait.PollImmediate(retryInterval, timeout, func() (bool, error) {
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

func CreateNamespace(name string) error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if err := Client.Create(context.Background(), namespace); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	return nil
}

func DeleteNamespaceSync(t *testing.T, name string, retryInterval, timeout time.Duration) error {
	t.Logf("deleting namespace %s", name)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	err := Client.Get(context.Background(), types.NamespacedName{Name: name}, namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		} else {
			return err
		}
	}

	if err = Client.Delete(context.Background(), namespace); err != nil {
		return err
	}

	return wait.Poll(retryInterval, timeout, func() (bool, error) {
		err := Client.Get(context.Background(), types.NamespacedName{Name: name}, namespace)

		if err == nil {
			return false, nil
		}

		if apierrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	})
}

// Blocks until the CassandraDatacenter is ready or until the timeout is reached. Readiness
// is determined by the Ready condition in the CassandraDatacenter status. An error is
// returned if fetching the CassandraDatacenter fails.
func WaitForCassandraDatacenterReady(t *testing.T, key types.NamespacedName, retryInterval, timeout time.Duration) error {
	//start := time.Now()
	return wait.PollImmediate(retryInterval, timeout, func() (bool, error) {
		cassdc, err := GetCassandraDatacenter(key)
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("cassandradatacenter %s not found", key)
				return false, nil
			}
			t.Logf("failed to get cassandradatacenter %s: %s", key, err)
			return true, err
		}
		//logCassDcStatus(t, cassdc, start)
		status := cassdc.GetConditionStatus(cassdcv1beta1.DatacenterReady)
		return status == corev1.ConditionTrue, nil
	})
}

func GetCassandraDatacenter(key types.NamespacedName) (*cassdcv1beta1.CassandraDatacenter, error) {
	cassdc := &cassdcv1beta1.CassandraDatacenter{}
	err := Client.Get(context.Background(), key, cassdc)

	return cassdc, err
}

func DeleteCassandraDatacenterSync(t *testing.T, key types.NamespacedName, retryInterval, timeout time.Duration) error {
	t.Logf("deleting cassandradatacenter %s", key)

	cassdc, err := GetCassandraDatacenter(key)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		} else {
			return err
		}
	}

	if err = Client.Delete(context.Background(), cassdc); err != nil {
		return err
	}

	return wait.Poll(retryInterval, timeout, func() (bool, error) {
		_, err := GetCassandraDatacenter(key)

		if err == nil {
			return false, nil
		}

		if apierrors.IsNotFound(err) {
			return true, nil
		}

		return false, err
	})
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

func WaitForRestoreToFinish(key types.NamespacedName, retryInterval, timeout time.Duration) error {
	return wait.Poll(retryInterval, timeout, func() (bool, error) {
		restore := &api.CassandraRestore{}
		err := Client.Get(context.Background(), key, restore)
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}
			return true, err
		}
		return !restore.Status.FinishTime.IsZero(), nil
	})
}
