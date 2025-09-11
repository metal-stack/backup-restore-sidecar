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

	onStartedLeading func(ctx context.Context)
	onStoppedLeading func()
	onNewLeader      func(identity string)
}

func New(config Config) (*LeaderElection, error) {
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("error creating in-cluster config: %v", err)
	}

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating kubernetes client: %v", err)
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
			return nil, fmt.Errorf("no POD_NAME environment variable")
		}
	}

	return &LeaderElection{
		log:         config.Log,
		client:      client,
		namespace:   namespace,
		podName:     podName,
		lockName:    config.LockName,
		onStarted:   config.onStartedLeading,
		onStopped:   config.onStoppedLeading,
		onNewLeader: config.onNewLeader,
	}, nil
}

func (le *LeaderElection) Start(ctx context.Context) error {
	le.log.Info("starting leader election,"+
		"namespace", le.namespace,
		"podName", le.podName,
		"lockName", le.lockName)

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
				le.log.Info("started leader election", "identity", le.podName)
				le.isLeader = true
				if le.onStarted != nil {
					le.onStarted(ctx)
				}
			},
			OnNewLeader: func(identity string) {
				if identity == le.podName {
					le.log.Info("acquired leadership", "identity", le.podName)
				} else {
					le.log.Info("new leader elected", "identity", identity)
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

func (le *LeaderElection) IsLeader() bool { return le.isLeader }
