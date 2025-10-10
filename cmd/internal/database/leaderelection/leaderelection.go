package leaderelection

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

type LeaderElection struct {
	log       *slog.Logger
	client    kubernetes.Interface
	namespace string
	podName   string
	lockName  string

	isLeader    bool
	onStarted   func(ctx context.Context)
	onStopped   func()
	onNewLeader func(identity string)
}

type Config struct {
	Log       *slog.Logger
	Namespace string
	PodName   string
	LockName  string

	OnStartedLeading func(ctx context.Context)
	OnStoppedLeading func()
	OnNewLeader      func(identity string)
}

func New(config Config) (*LeaderElection, error) {
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	namespace := config.Namespace
	if namespace == "" {
		namespace = os.Getenv("POD_NAMESPACE")
		if namespace == "" {
			namespace = "default"
		}
	}

	podName := config.PodName
	if podName == "" {
		podName = os.Getenv("POD_NAME")
		if podName == "" {
			return nil, fmt.Errorf("pod name is required")
		}
	}

	return &LeaderElection{
		log:         config.Log,
		client:      client,
		namespace:   namespace,
		podName:     podName,
		onStarted:   config.OnStartedLeading,
		onStopped:   config.OnStoppedLeading,
		onNewLeader: config.OnNewLeader,
	}, nil
}

func (le *LeaderElection) Start(ctx context.Context) error {
	le.log.Info("starting leader election",
		"namespace", le.namespace,
		"podName", le.podName)

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      le.lockName,
			Namespace: le.namespace,
		},
		Client: le.client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: le.podName,
		},
	}

	leaderElectionConfig := leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: 60 * time.Second,
		RenewDeadline: 15 * time.Second,
		RetryPeriod:   5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				le.log.Info("started leading", "identity", le.podName)
				le.isLeader = true
				if le.onStarted != nil {
					le.onStarted(ctx)
				}
			},
			OnStoppedLeading: func() {
				le.log.Info("stopped leading", "identity", le.podName)
				le.isLeader = false
				if le.onStopped != nil {
					le.onStopped()
				}
			},
			OnNewLeader: func(identity string) {
				if identity == le.podName {
					le.log.Info("successfully acquired leadership", "identity", identity)
				} else {
					le.log.Info("new leader elected", "leader", identity, "self", le.podName)
				}
				if le.onNewLeader != nil {
					le.onNewLeader(identity)
				}
			},
		},
	}

	leaderelection.RunOrDie(ctx, leaderElectionConfig)
	return nil
}

func (le *LeaderElection) IsLeader() bool {
	return le.isLeader
}
