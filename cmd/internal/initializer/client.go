package initializer

import (
	"context"
	"fmt"
	"net/url"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// NewInitializerClient returns a new initializer client.
func NewInitializerClient(ctx context.Context, rawurl string, log *zap.SugaredLogger, stop <-chan struct{}) (v1.InitializerServiceClient, error) {
	parsedurl, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	if parsedurl.Host == "" {
		return nil, fmt.Errorf("invalid url:%s, must be in the form scheme://host[:port]/basepath", rawurl)
	}

	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithBlock(),
	}

	conn, err := grpc.DialContext(ctx, parsedurl.Host, opts...)
	if err != nil {
		return nil, err
	}

	return v1.NewInitializerServiceClient(conn), nil
}
