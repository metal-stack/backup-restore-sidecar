module github.com/metal-stack/backup-restore-sidecar

go 1.16

require (
	cloud.google.com/go/storage v1.16.0
	github.com/aws/aws-sdk-go v1.40.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.3.0
	github.com/kr/text v0.2.0 // indirect
	github.com/metal-stack/v v1.0.3
	github.com/mholt/archiver/v3 v3.5.0
	github.com/olekukonko/tablewriter v0.0.5
	github.com/prometheus/client_golang v1.11.0
	github.com/robfig/cron/v3 v3.0.1
	github.com/spf13/cobra v1.2.1
	github.com/spf13/viper v1.8.1
	go.uber.org/zap v1.18.1
	google.golang.org/api v0.50.0
	google.golang.org/grpc v1.39.0
	google.golang.org/protobuf v1.27.1
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)
