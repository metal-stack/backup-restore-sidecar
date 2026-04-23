package metrics

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics contains the collected metrics
type Metrics struct {
	totalBackups        prometheus.Counter
	backupSuccess       prometheus.Gauge
	databaseInitialized prometheus.Gauge
	backupSize          prometheus.Gauge
	totalErrors         *prometheus.CounterVec
}

// New generates new metrics
func New() *Metrics {
	backupSuccess := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "backup_success",
		Help: "is 1 when the last backup was successful, otherwise 0",
	},
	)

	databaseInitialized := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "backup_database_initialized",
		Help: "is 1 when the database is initialized for backups and a database probe has succeeded once, 0 otherwise",
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
		totalBackups:        totalBackups,
		backupSuccess:       backupSuccess,
		databaseInitialized: databaseInitialized,
		totalErrors:         totalErrors,
		backupSize:          backupSize,
	}
}

// Start starts the metrics server
func (m *Metrics) Start(log *slog.Logger) {
	log.Info("starting metrics server")
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`<html>
			<head><title>backup-restore metrics</title></head>
			<body>
			<h1>backup-restore metrics</h1>
			<p><a href='/metrics'>Metrics</a></p </body </html>`))
		if err != nil {
			log.Error("error handling metrics root endpoint", "error", err)
		}
	})

	prometheus.MustRegister(m.backupSuccess)
	prometheus.MustRegister(m.databaseInitialized)
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
			panic(err)
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

// SetDatabaseInitialized updates the database initialized metric
func (m *Metrics) SetDatabaseInitialized(initialized bool) {
	if initialized {
		m.databaseInitialized.Set(1)
	} else {
		m.databaseInitialized.Set(0)
	}
}
