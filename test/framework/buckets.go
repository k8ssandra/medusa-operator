package framework

import (
	"fmt"
	"os/exec"
	"github.com/gruntwork-io/terratest/modules/shell"
	"github.com/gruntwork-io/terratest/modules/logger"
	"testing"
)

type BucketObjectDeleter interface {
	DeleteObjects(t *testing.T, bucket string) (string, error)

	//DeleteObjectsWithPrefix(bucket, prefix string) (string, error)
}

type s3ObjectDeleter struct {
}

func NewBucketObjectDeleter(storageType string) (BucketObjectDeleter, error) {
	switch storageType {
	case "aws":
		if _, err := exec.LookPath("aws"); err != nil {
			return nil, err
		}
		return &s3ObjectDeleter{}, nil
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", storageType)
	}
}

func (d *s3ObjectDeleter) DeleteObjects(t *testing.T, bucket string) (string, error) {
	cmd := shell.Command{
		Command: "aws",
		Args: []string{"s3", "rm", "s3://" + bucket, "--recursive"},
		Logger: logger.Discard,
	}
	return shell.RunCommandAndGetOutputE(t, cmd)
}
