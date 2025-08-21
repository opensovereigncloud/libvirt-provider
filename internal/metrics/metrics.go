// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"errors"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/util/workqueue"
)

const (
	namespace = "libvirtprovider"

	subsystemControllerRuntime = "controller_runtime"
	subsystemOperation         = "operation"
	subsystemWorkQueue         = "workqueue"

	LabelName                 = "name"
	LabelController           = "controller"
	LabelOperation            = "operation"
	LabelEventID              = "event_id"
	LabelEventType            = "event_type"
	LabelServer               = "server"
	LabelMethod               = "method"
	LabelPath                 = "path"
	LabelStatus               = "status"
	LabelState                = "state"
	LabelMachineclass         = "machineclass"
	LabelSource               = "source"
	LabelResource             = "resource"
	LabelMachineID            = "machine_id"
	LabelNic                  = "nic"
	LabelRootMachineName      = "root_machine_name"
	LabelRootMachineNamespace = "root_machine_namespace"

	LabelValueUnknown = "unknown"

	LogKeyLabels = "labels"
)

var ErrNilMetric = errors.New("metric is nil")

var (
	ControllerRuntimeReconcileErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: subsystemControllerRuntime,
		Name:      "reconcile_errors_total",
		Help:      "Total number of reconciliation errors per controller",
	}, []string{LabelController})

	ControllerRuntimeReconcileDuration = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: subsystemControllerRuntime,
		Name:      "reconcile_duration_seconds",
		Help:      "Length of time per reconciliation per controller",
	}, []string{LabelController})

	ControllerRuntimeMaxConcurrentReconciles = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemControllerRuntime,
		Name:      "max_concurrent_reconciles",
		Help:      "Maximum number of concurrent reconciles per controller",
	}, []string{LabelController})

	ControllerRuntimeActiveWorker = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemControllerRuntime,
		Name:      "active_workers",
		Help:      "Number of currently used workers per controller",
	}, []string{LabelController})

	workqueueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "depth",
		Help:      "Current depth of workqueue",
	}, []string{LabelName})

	workqueueAdds = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "adds_total",
		Help:      "Total number of adds handled by workqueue",
	}, []string{LabelName})

	workqueueLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "queue_duration_seconds",
		Help:      "How long in seconds an item stays in workqueue before being requested",
		Buckets:   prometheus.ExponentialBuckets(10e-9, 10, 12),
	}, []string{LabelName})

	workqueueDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "work_duration_seconds",
		Help:      "How long in seconds processing an item from workqueue takes.",
		Buckets:   prometheus.ExponentialBuckets(10e-9, 10, 12),
	}, []string{LabelName})

	workqueueUnfinished = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "unfinished_work_seconds",
		Help: "How many seconds of work has been done that " +
			"is in progress and hasn't been observed by work_duration. Large " +
			"values indicate stuck threads. One can deduce the number of stuck " +
			"threads by observing the rate at which this increases.",
	}, []string{LabelName})

	workqueueLongestRunningProcessor = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "longest_running_processor_seconds",
		Help: "How many seconds has the longest running " +
			"processor for workqueue been running.",
	}, []string{LabelName})

	workqueueRetries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "retries_total",
		Help:      "Total number of retries handled by workqueue",
	}, []string{LabelName})

	OperationDuration = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: subsystemOperation,
		Name:      "duration_seconds",
		Help:      "Length of time per operation",
	}, []string{LabelOperation})

	OperationErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: subsystemOperation,
		Name:      "errors_total",
		Help:      "Total number of errors which affect main logic of operation",
	}, []string{LabelOperation})
)

func RegisterAllMetrics() error {
	collectors := []prometheus.Collector{
		ControllerRuntimeReconcileErrors,
		ControllerRuntimeReconcileDuration,
		ControllerRuntimeMaxConcurrentReconciles,
		ControllerRuntimeActiveWorker,

		OperationDuration,
		OperationErrors,

		workqueueDepth,
		workqueueAdds,
		workqueueLatency,
		workqueueDuration,
		workqueueUnfinished,
		workqueueLongestRunningProcessor,
		workqueueRetries,

		IRIEventsOverriddenTotal,
		IRIEventsBufferUsageRatio,
		LibvirtEventsCount,

		httpServerRequestDuration,
		httpServerTotalRequests,

		MachinesDeleteMarked,
		MachinesState,
		MachinesDestroyed,
		PanicsRecovered,

		ResourcesAvailable,
		ResourcesTotal,
		VMSlotsAvailable,
		MachineClassesSkipped,
		MachinesAvailable,
		MachinesTotal,

		APINetNicsVirtFn,
	}

	for i := range collectors {
		err := prometheus.Register(collectors[i])
		if err != nil {
			return err
		}
	}

	return nil
}

type WorkqueueMetricsProvider struct {
	log logr.Logger
}

func NewWorkqueueMetricsProvider(log logr.Logger) *WorkqueueMetricsProvider {
	return &WorkqueueMetricsProvider{log: log}
}

func (w *WorkqueueMetricsProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	labels := prometheus.Labels{LabelName: name}
	metric, err := GetGaugeWithLabels(workqueueDepth, labels)
	if err != nil {
		w.log.Error(err, "failed to get workqueue depth metric", LogKeyLabels, labels)
	}
	return metric
}

func (w *WorkqueueMetricsProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	labels := prometheus.Labels{LabelName: name}
	metric, err := GetCounterWithLabels(workqueueAdds, labels)
	if err != nil {
		w.log.Error(err, "failed to get workqueue adds metric", LogKeyLabels, labels)
	}
	return metric
}

func (w *WorkqueueMetricsProvider) NewLatencyMetric(name string) workqueue.HistogramMetric {
	labels := prometheus.Labels{LabelName: name}
	metric, err := GetHistogramWithLabels(workqueueLatency, labels)
	if err != nil {
		w.log.Error(err, "failed to get workqueue latency metric", LogKeyLabels, labels)
	}
	return metric
}

func (w *WorkqueueMetricsProvider) NewWorkDurationMetric(name string) workqueue.HistogramMetric {
	labels := prometheus.Labels{LabelName: name}
	metric, err := GetHistogramWithLabels(workqueueDuration, labels)
	if err != nil {
		w.log.Error(err, "failed to get workqueue work duration metric", LogKeyLabels, labels)
	}
	return metric
}

func (w *WorkqueueMetricsProvider) NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	labels := prometheus.Labels{LabelName: name}
	metric, err := GetGaugeWithLabels(workqueueUnfinished, labels)
	if err != nil {
		w.log.Error(err, "failed to get workqueue unfinished work seconds metric", LogKeyLabels, labels)
	}
	return metric
}

func (w *WorkqueueMetricsProvider) NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric {
	labels := prometheus.Labels{LabelName: name}
	metric, err := GetGaugeWithLabels(workqueueLongestRunningProcessor, labels)
	if err != nil {
		w.log.Error(err, "failed to get workqueue longest running processor seconds metric", LogKeyLabels, labels)
	}
	return metric
}

func (w *WorkqueueMetricsProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	labels := prometheus.Labels{LabelName: name}
	metric, err := GetCounterWithLabels(workqueueRetries, labels)
	if err != nil {
		w.log.Error(err, "failed to get workqueue retries metric", LogKeyLabels, labels)
	}
	return metric
}

func GetCounterWithLabels(metric *prometheus.CounterVec, labels prometheus.Labels) (prometheus.Counter, error) {
	if metric == nil {
		return NoOpsMetricProvider, ErrNilMetric
	}

	counter, err := metric.GetMetricWith(prometheus.Labels(labels))
	if err != nil {
		return NoOpsMetricProvider, err
	}
	return counter, nil
}

func GetGaugeWithLabels(metric *prometheus.GaugeVec, labels prometheus.Labels) (prometheus.Gauge, error) {
	if metric == nil {
		return NoOpsMetricProvider, ErrNilMetric
	}

	gauge, err := metric.GetMetricWith(prometheus.Labels(labels))
	if err != nil {
		return NoOpsMetricProvider, err
	}
	return gauge, nil
}

func GetHistogramWithLabels(metric *prometheus.HistogramVec, labels prometheus.Labels) (prometheus.Observer, error) {
	if metric == nil {
		return NoOpsMetricProvider, ErrNilMetric
	}

	histogram, err := metric.GetMetricWith(prometheus.Labels(labels))
	if err != nil {
		return NoOpsMetricProvider, err
	}
	return histogram, nil
}

func GetSummaryWithLabels(metric *prometheus.SummaryVec, labels prometheus.Labels) (prometheus.Observer, error) {
	if metric == nil {
		return NoOpsMetricProvider, ErrNilMetric
	}

	summary, err := metric.GetMetricWith(prometheus.Labels(labels))
	if err != nil {
		return NoOpsMetricProvider, err
	}
	return summary, nil
}
