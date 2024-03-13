package examples

const (
	backupRestoreSidecarContainerImage = "ghcr.io/metal-stack/backup-restore-sidecar:latest"
)

func pointer[T any](t T) *T {
	return &t
}
