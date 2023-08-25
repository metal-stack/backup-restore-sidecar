package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// taken from https://github.com/gianarb/kube-port-forward

type portForwardAPodRequest struct {
	// RestConfig is the kubernetes config
	RestConfig *rest.Config
	// Pod is the selected pod for this port forwarding
	Pod v1.Pod
	// LocalPort is the local port that will be selected to expose the PodPort
	LocalPort int
	// PodPort is the target port for the pod
	PodPort int
	// Steams configures where to write or read input from
	Streams genericclioptions.IOStreams
	// StopCh is the channel used to manage the port forward lifecycle
	StopCh <-chan struct{}
	// ReadyCh communicates when the tunnel is ready to receive traffic
	ReadyCh chan struct{}
}

func portForwardAPod(req portForwardAPodRequest) error {
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward",
		req.Pod.Namespace, req.Pod.Name)
	hostIP := strings.TrimLeft(req.RestConfig.Host, "htps:/")

	transport, upgrader, err := spdy.RoundTripperFor(req.RestConfig)
	if err != nil {
		return err
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, &url.URL{Scheme: "https", Path: path, Host: hostIP})
	fw, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", req.LocalPort, req.PodPort)}, req.StopCh, req.ReadyCh, req.Streams.Out, req.Streams.ErrOut)
	if err != nil {
		return err
	}
	return fw.ForwardPorts()
}

type runInPodForwardingRequest struct {
	// RestConfig is the kubernetes config
	RestConfig *rest.Config
	// Pod is the selected pod for this port forwarding
	Pod v1.Pod
	// LocalPort is the local port that will be selected to expose the PodPort
	LocalPort int
	// PodPort is the target port for the pod
	PodPort int
}

func runInPodForwarding(req runInPodForwardingRequest, fn func()) {
	stopCh := make(chan struct{}, 1)
	readyCh := make(chan struct{})
	stream := genericclioptions.IOStreams{
		// for debugging:
		// In:     os.Stdout,
		// Out:    os.Stdout,
		// ErrOut: os.Stdout,
		In:     nil,
		Out:    io.Discard,
		ErrOut: io.Discard,
	}

	go func() {
		err := portForwardAPod(portForwardAPodRequest{
			RestConfig: restConfig,
			Pod:        req.Pod,
			LocalPort:  req.LocalPort,
			PodPort:    req.PodPort,
			Streams:    stream,
			StopCh:     stopCh,
			ReadyCh:    readyCh,
		})
		if err != nil {
			panic(err)
		}
	}()

	<-readyCh

	fn()

	stopCh <- struct{}{}
}
