package medusa

import (
	"context"
	"fmt"
	"google.golang.org/grpc"

	"github.com/k8ssandra/medusa-operator/pkg/pb"
)

type Client struct {
	connection *grpc.ClientConn
	grpcClient pb.MedusaClient
}

func NewClient(address string) (*Client, error) {
	conn, err := grpc.Dial(address, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithDefaultCallOptions(grpc.WaitForReady(false)))

	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to %s: %s", address, err)
	}

	return &Client{connection: conn, grpcClient: pb.NewMedusaClient(conn)}, nil
}

func (c *Client) Close() error {
	return c.connection.Close()
}

func (c *Client) CreateBackup(ctx context.Context, name string) error {
	request := pb.BackupRequest{
		Name: name,
		Mode: pb.BackupRequest_DIFFERENTIAL,
	}
	_, err := c.grpcClient.Backup(ctx, &request)

	return err
}

func (c *Client) GetBackups(ctx context.Context) ([]*pb.BackupSummary, error) {
	response, err := c.grpcClient.GetBackups(ctx, &pb.GetBackupsRequest{})
	if err != nil {
		return nil, fmt.Errorf("failed to get backups: %s", err)
	}
	return response.Backups, nil
}

func (c *Client) DeleteBackup(ctx context.Context, name string) error {
	request := pb.DeleteBackupRequest{Name: name}
	_, err := c.grpcClient.DeleteBackup(context.Background(), &request)
	return err
}
