package initializer

import (
	"fmt"
	"net/url"
	"time"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

func NewInitializerClientWithRetry(rawurl string, log *zap.SugaredLogger) (v1.InitializerServiceClient, error) {
	parsedurl, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}
	if parsedurl.Host == "" {
		return nil, fmt.Errorf("invalid url:%s, must be in the form scheme://host[:port]/basepath", rawurl)
	}

	opts := []grpc.DialOption{
		grpc.WithTimeout(3 * time.Second),
		grpc.WithInsecure(),
		grpc.WithBlock(),
	}

	var conn *grpc.ClientConn
	for {
		conn, err = grpc.Dial(parsedurl.Host, opts...)
		if err != nil {
			log.Errorw("client did not connect, retrying...", "error", err)
			continue
		}
		break
	}

	return v1.NewInitializerServiceClient(conn), nil
}
