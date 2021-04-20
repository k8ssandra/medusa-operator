package framework

import (
	"fmt"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/shell"
	"os/exec"
	"testing"
)

type BucketObjectDeleter interface {
	DeleteObjects(t *testing.T, bucket string) (string, error)

	//DeleteObjectsWithPrefix(bucket, prefix string) (string, error)
}

type BucketManager interface {
	CreateBucket(t *testing.T, bucket string, args ...string) (string, error)

	DeleteBucket(t *testing.T, bucket string) (string, error)
}

type s3Manager struct{}

type s3ObjectDeleter struct{}

type gcsObjectDeleter struct{}

func NewBucketObjectDeleter(storageType string) (BucketObjectDeleter, error) {
	switch storageType {
	case "s3":
		if _, err := exec.LookPath("aws"); err != nil {
			return nil, err
		}
		return &s3ObjectDeleter{}, nil
	case "gcs":
		if _, err := exec.LookPath("gsutil"); err != nil {
			return nil, err
		}
		return &gcsObjectDeleter{}, nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}
}

//func NewBucketManager(storageType string) (BucketManager, error) {
//	switch storageType {
//	case "s3":
//		if _, err := exec.LookPath("aws"); err != nil {
//			return nil, fmt.Errorf("aws cli must be installed on PATH for s3 storage type: %s", err)
//		}
//		return &s3Manager{}, nil
//	default:
//	}
//}

func (mgr *s3Manager) CreateBucket(t *testing.T, bucket string, args ...string) (string, error) {
	cmdArgs := []string{"aws", "s3api", "create-bucket", "--bucket", bucket}
	cmdArgs = append(cmdArgs, args...)

	cmd := shell.Command{
		Command: "aws",
		Args: cmdArgs,
		Logger: logger.Discard,
	}
	return shell.RunCommandAndGetOutputE(t, cmd)
}

func (mgr *s3Manager) DeleteBucket(t *testing.T, bucket string) (string, error) {
	cmd := shell.Command{
		Command: "aws",
		Args: []string{"s3", "rb", "s3://" + bucket, "--force"},
		Logger: logger.Discard,
	}
	return shell.RunCommandAndGetOutputE(t, cmd)
}

func (d *s3ObjectDeleter) DeleteObjects(t *testing.T, bucket string) (string, error) {
	cmd := shell.Command{
		Command: "aws",
		Args:    []string{"s3", "rm", "s3://" + bucket, "--recursive"},
		Logger:  logger.Discard,
	}
	return shell.RunCommandAndGetOutputE(t, cmd)
}

func (d *gcsObjectDeleter) DeleteObjects(t *testing.T, bucket string) (string, error) {
	if empty, err := d.isBucketEmpty(t, bucket); err == nil {
		if empty {
			return "", nil
		} else {
			cmd := shell.Command{
				Command: "gsutil",
				Args:    []string{"-m", "rm", "-r", "gs://" + bucket + "/*"},
				//Args: []string{"rm", "-r", "gs://" + bucket + "/*"},
				Logger: logger.Discard,
			}
			return shell.RunCommandAndGetOutputE(t, cmd)
		}
	} else {
		return "", err
	}
}

func (d *gcsObjectDeleter) isBucketEmpty(t *testing.T, bucket string) (bool, error) {
	cmd := shell.Command{
		Command: "gsutil",
		Args:    []string{"ls", "gs://" + bucket},
		Logger:  logger.Discard,
	}
	if output, err := shell.RunCommandAndGetOutputE(t, cmd); err == nil {
		return len(output) == 0, nil
	} else {
		return false, err
	}
}
