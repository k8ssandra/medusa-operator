package medusa

import (
	"context"
	"fmt"
	"google.golang.org/grpc"

	"github.com/k8ssandra/medusa-operator/pkg/pb"
)

type defaultClient struct {
	connection *grpc.ClientConn
	grpcClient pb.MedusaClient
}

type ClientFactory interface {
	NewClient(address string) (Client, error)
}

type DefaultFactory struct {
}

func (f *DefaultFactory) NewClient(address string) (Client, error) {
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithDefaultCallOptions(grpc.WaitForReady(false)))

	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to %s: %s", address, err)
	}

	return &defaultClient{connection: conn, grpcClient: pb.NewMedusaClient(conn)}, nil
}

type Client interface {
	Close() error

	CreateBackup(ctx context.Context, name string) error

	GetBackups(ctx context.Context) ([]*pb.BackupSummary, error)
}

func (c *defaultClient) Close() error {
	return c.connection.Close()
}

func (c *defaultClient) CreateBackup(ctx context.Context, name string) error {
	request := pb.BackupRequest{
		Name: name,
		Mode: pb.BackupRequest_DIFFERENTIAL,
	}
	_, err := c.grpcClient.Backup(ctx, &request)

	return err
}

func (c *defaultClient) GetBackups(ctx context.Context) ([]*pb.BackupSummary, error) {
	response, err := c.grpcClient.GetBackups(ctx, &pb.GetBackupsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get backups: %s", err)
	}
	return response.Backups, nil
}

func (c *defaultClient) DeleteBackup(ctx context.Context, name string) error {
	request := pb.DeleteBackupRequest{Name: name}
	_, err := c.grpcClient.DeleteBackup(context.Background(), &request)
	return err
}
