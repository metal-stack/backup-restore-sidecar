package metrics

import (
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Metrics contains the collected metrics
type Metrics struct {
	totalBackups  prometheus.Counter
	backupSuccess prometheus.Gauge
	backupSize    prometheus.Gauge
	totalErrors   *prometheus.CounterVec
}

// New generates new metrics
func New() *Metrics {
	backupSuccess := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "backup_success",
		Help: "is 0 when the last backup was successful, otherwise 1",
	},
	)

	totalBackups := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "backup_total_backups",
		Help: "total number of successful backups",
	},
	)

	totalErrors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backup_errors",
		Help: "total number of errors during backups",
	},
		[]string{"operation"},
	)

	backupSize := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "backup_size",
		Help: "size of last backup in bytes",
	},
	)

	return &Metrics{
		totalBackups:  totalBackups,
		backupSuccess: backupSuccess,
		totalErrors:   totalErrors,
		backupSize:    backupSize,
	}
}

// Start starts the metrics server
func (m *Metrics) Start(log *zap.SugaredLogger) {
	log.Info("starting metrics server")
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`<html>
			<head><title>backup-restore metrics</title></head>
			<body>
			<h1>backup-restore metrics</h1>
			<p><a href='/metrics'>Metrics</a></p </body </html>`))
		if err != nil {
			log.Errorw("error handling metrics root endpoint", "error", err)
		}
	})

	prometheus.MustRegister(m.backupSuccess)
	prometheus.MustRegister(m.totalBackups)
	prometheus.MustRegister(m.totalErrors)
	prometheus.MustRegister(m.backupSize)

	go func() {
		server := http.Server{
			Addr:              ":2112",
			ReadHeaderTimeout: 1 * time.Minute,
		}
		err := server.ListenAndServe()
		if err != nil {
			log.Fatal(err)
		}
	}()
}

// CountBackup updates metrics counter
func (m *Metrics) CountBackup(backupFile string) {
	s, _ := os.Stat(backupFile)
	m.totalBackups.Inc()
	m.backupSuccess.Set(1)
	m.backupSize.Set(float64(s.Size()))
}

// CountError increases error counter for the given operation
func (m *Metrics) CountError(op string) {
	m.totalErrors.With(prometheus.Labels{"operation": op}).Inc()
	m.backupSuccess.Set(0)
}
