// Package kubernetes taken from https://github.com/micro/go-micro/blob/master/debug/log/kubernetes/kubernetes.go
// There are some modifications compared to the other files as
// this package doesn't provide write functionality.
// With the write functionality gone, structured logs also go away.
package kubernetes

import (
	"bufio"
	"strconv"
	"time"

	"go-micro.org/v4/logger"
	"go-micro.org/v4/runtime"
	"go-micro.org/v4/util/kubernetes/client"
)

type klog struct {
	options     runtime.LogsOptions
	client      client.Client
	serviceName string
}

func (k *klog) podLogStream(podName string, stream *kubeStream) error {
	p := make(map[string]string)
	p["follow"] = "true"

	opts := []client.LogOption{
		client.LogParams(p),
		client.LogNamespace(k.options.Namespace),
	}

	// get the logs for the pod
	body, err := k.client.Log(&client.Resource{
		Name: podName,
		Kind: "pod",
	}, opts...)

	if err != nil {
		stream.err = err
		if err := stream.Stop(); err != nil {
			stream.err = err
			return err
		}

		return err
	}

	s := bufio.NewScanner(body)
	defer body.Close()

	for {
		select {
		case <-stream.stop:
			return stream.Error()
		default:
			if s.Scan() {
				record := runtime.LogRecord{
					Message: s.Text(),
				}
				stream.stream <- record
			} else {
				// TODO: is there a blocking call
				// rather than a sleep loop?
				time.Sleep(time.Second)
			}
		}
	}
}

func (k *klog) getMatchingPods() ([]string, error) {
	r := &client.Resource{
		Kind:  "pod",
		Value: new(client.PodList),
	}

	l := make(map[string]string)

	l["name"] = client.Format(k.serviceName)
	// TODO: specify micro:service
	// l["micro"] = "service"

	opts := []client.GetOption{
		client.GetLabels(l),
		client.GetNamespace(k.options.Namespace),
	}

	if err := k.client.Get(r, opts...); err != nil {
		return nil, err
	}

	var matches []string

	podList, ok := r.Value.(*client.PodList)
	if !ok {
		logger.DefaultLogger.Log(logger.ErrorLevel, "Failed to cast to *client.PodList")
	}

	for _, p := range podList.Items {
		// find labels that match the name
		if p.Metadata.Labels["name"] == client.Format(k.serviceName) {
			matches = append(matches, p.Metadata.Name)
		}
	}

	return matches, nil
}

func (k *klog) Read() ([]runtime.LogRecord, error) {
	pods, err := k.getMatchingPods()
	if err != nil {
		return nil, err
	}

	var records []runtime.LogRecord

	for _, pod := range pods {
		logParams := make(map[string]string)

		// if !opts.Since.Equal(time.Time{}) {
		//	logParams["sinceSeconds"] = strconv.Itoa(int(time.Since(opts.Since).Seconds()))
		//}

		if k.options.Count != 0 {
			logParams["tailLines"] = strconv.Itoa(int(k.options.Count))
		}

		if k.options.Stream {
			logParams["follow"] = "true"
		}

		opts := []client.LogOption{
			client.LogParams(logParams),
			client.LogNamespace(k.options.Namespace),
		}

		logs, err := k.client.Log(&client.Resource{
			Name: pod,
			Kind: "pod",
		}, opts...)

		if err != nil {
			return nil, err
		}
		defer logs.Close()

		s := bufio.NewScanner(logs)

		for s.Scan() {
			record := runtime.LogRecord{
				Message: s.Text(),
			}
			// record.Metadata["pod"] = pod
			records = append(records, record)
		}
	}

	// sort the records
	// sort.Slice(records, func(i, j int) bool { return records[i].Timestamp.Before(records[j].Timestamp) })

	return records, nil
}

func (k *klog) Stream() (runtime.LogStream, error) {
	// find the matching pods
	pods, err := k.getMatchingPods()
	if err != nil {
		return nil, err
	}

	stream := &kubeStream{
		stream: make(chan runtime.LogRecord),
		stop:   make(chan bool),
	}

	// stream from the individual pods
	for _, pod := range pods {
		go func(podName string) {
			err := k.podLogStream(podName, stream)
			if err != nil {
				logger.DefaultLogger.Log(logger.ErrorLevel, err)
			}
		}(pod)
	}

	return stream, nil
}

// NewLog returns a configured Kubernetes logger.
func newLog(c client.Client, serviceName string, opts ...runtime.LogsOption) *klog {
	options := runtime.LogsOptions{
		Namespace: client.DefaultNamespace,
	}
	for _, o := range opts {
		o(&options)
	}

	klog := &klog{
		serviceName: serviceName,
		client:      c,
		options:     options,
	}

	return klog
}
