module github.com/metal-stack/backup-restore-sidecar

go 1.15

require (
	cloud.google.com/go/storage v1.12.0
	github.com/aws/aws-sdk-go v1.35.3
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/golang/protobuf v1.4.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.2
	github.com/metal-stack/v v1.0.2
	github.com/mholt/archiver/v3 v3.3.2
	github.com/mitchellh/mapstructure v1.3.2 // indirect
	github.com/olekukonko/tablewriter v0.0.4
	github.com/pelletier/go-toml v1.8.1 // indirect
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.7.1
	github.com/robfig/cron/v3 v3.0.1
	github.com/spf13/afero v1.3.0 // indirect
	github.com/spf13/cast v1.3.1 // indirect
	github.com/spf13/cobra v1.0.0
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/spf13/viper v1.7.1
	go.uber.org/zap v1.16.0
	google.golang.org/api v0.32.0
	google.golang.org/grpc v1.32.0
)
