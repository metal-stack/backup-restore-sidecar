package client

import (
	"context"
	"fmt"
	"net/url"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client interface {
	InitializerServiceClient() v1.InitializerServiceClient
	BackupServiceClient() v1.BackupServiceClient
	DatabaseServiceClient() v1.DatabaseServiceClient
}

type client struct {
	conn *grpc.ClientConn
}

// New returns a new backup-restore-sidecar grpc client.
func New(ctx context.Context, rawurl string) (Client, error) {
	parsedurl, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	if parsedurl.Host == "" {
		return nil, fmt.Errorf("invalid url:%s, must be in the form scheme://host[:port]/basepath", rawurl)
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	}

	conn, err := grpc.DialContext(ctx, parsedurl.Host, opts...)
	if err != nil {
		return nil, err
	}

	return &client{conn: conn}, nil
}

func (c *client) InitializerServiceClient() v1.InitializerServiceClient {
	return v1.NewInitializerServiceClient(c.conn)
}

func (c *client) BackupServiceClient() v1.BackupServiceClient {
	return v1.NewBackupServiceClient(c.conn)
}

func (c *client) DatabaseServiceClient() v1.DatabaseServiceClient {
	return v1.NewDatabaseServiceClient(c.conn)
}
