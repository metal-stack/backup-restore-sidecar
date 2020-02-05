package initializer

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	v1 "github.com/metal-stack/backup-restore-sidecar/api/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// NewInitializerClientWithRetry returns a new initializer client when a connection to the initializer server has been established.
func NewInitializerClientWithRetry(rawurl string, log *zap.SugaredLogger, stop <-chan struct{}) (v1.InitializerServiceClient, error) {
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
		select {
		case <-stop:
			return nil, errors.New("received stop signal, stop establishing initializer client")
		default:
			conn, err = grpc.Dial(parsedurl.Host, opts...)
			if err == nil {
				return v1.NewInitializerServiceClient(conn), nil
			}
			log.Errorw("client did not connect, retrying...", "error", err)
		}
	}
}
