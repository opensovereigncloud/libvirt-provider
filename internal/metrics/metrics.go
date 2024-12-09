// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/util/workqueue"
)

const (
	subsystemControllerRuntime = "controller_runtime"
	subsystemOperation         = "operation"
	subsystemWorkQueue         = "workqueue"
)

var (
	ControllerRuntimeReconcileErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: subsystemControllerRuntime,
		Name:      "reconcile_errors_total",
		Help:      "Total number of reconciliation errors per controller",
	}, []string{"controller"})

	ControllerRuntimeReconcileDuration = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: subsystemControllerRuntime,
		Name:      "reconcile_duration_seconds",
		Help:      "Length of time per reconciliation per controller",
	}, []string{"controller"})

	ControllerRuntimeMaxConccurrentReconciles = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemControllerRuntime,
		Name:      "max_concurrent_reconciles",
		Help:      "Maximum number of concurrent reconciles per controller",
	}, []string{"controller"})

	ControllerRuntimeActiveWorker = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemControllerRuntime,
		Name:      "active_workers",
		Help:      "Number of currently used workers per controller",
	}, []string{"controller"})

	workqueueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "depth",
		Help:      "Current depth of workqueue",
	}, []string{"name"})

	workqueueAdds = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "adds_total",
		Help:      "Total number of adds handled by workqueue",
	}, []string{"name"})

	workqueueLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "queue_duration_seconds",
		Help:      "How long in seconds an item stays in workqueue before being requested",
		Buckets:   prometheus.ExponentialBuckets(10e-9, 10, 12),
	}, []string{"name"})

	workqueueDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "work_duration_seconds",
		Help:      "How long in seconds processing an item from workqueue takes.",
		Buckets:   prometheus.ExponentialBuckets(10e-9, 10, 12),
	}, []string{"name"})

	workqueueUnfinished = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "unfinished_work_seconds",
		Help: "How many seconds of work has been done that " +
			"is in progress and hasn't been observed by work_duration. Large " +
			"values indicate stuck threads. One can deduce the number of stuck " +
			"threads by observing the rate at which this increases.",
	}, []string{"name"})

	workqueueLongestRunningProcessor = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "longest_running_processor_seconds",
		Help: "How many seconds has the longest running " +
			"processor for workqueue been running.",
	}, []string{"name"})

	workqueueRetries = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: subsystemWorkQueue,
		Name:      "retries_total",
		Help:      "Total number of retries handled by workqueue",
	}, []string{"name"})

	OperationDuration = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Subsystem: subsystemOperation,
		Name:      "duration_seconds",
		Help:      "Length of time per operation",
	}, []string{"operation"})

	OperationErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Subsystem: subsystemOperation,
		Name:      "errors_total",
		Help:      "Total number of errors which affect main logic of operation",
	}, []string{"operation"})
)

func init() {
	prometheus.MustRegister(ControllerRuntimeReconcileErrors)
	prometheus.MustRegister(ControllerRuntimeReconcileDuration)
	prometheus.MustRegister(ControllerRuntimeMaxConccurrentReconciles)
	prometheus.MustRegister(ControllerRuntimeActiveWorker)

	prometheus.MustRegister(OperationDuration)
	prometheus.MustRegister(OperationErrors)

	prometheus.MustRegister(workqueueDepth)
	prometheus.MustRegister(workqueueAdds)
	prometheus.MustRegister(workqueueLatency)
	prometheus.MustRegister(workqueueDuration)
	prometheus.MustRegister(workqueueUnfinished)
	prometheus.MustRegister(workqueueLongestRunningProcessor)
	prometheus.MustRegister(workqueueRetries)
	workqueue.SetProvider(WorkqueueMetricsProvider{})
}

type WorkqueueMetricsProvider struct{}

func (WorkqueueMetricsProvider) NewDepthMetric(name string) workqueue.GaugeMetric {
	return workqueueDepth.WithLabelValues(name)
}

func (WorkqueueMetricsProvider) NewAddsMetric(name string) workqueue.CounterMetric {
	return workqueueAdds.WithLabelValues(name)
}

func (WorkqueueMetricsProvider) NewLatencyMetric(name string) workqueue.HistogramMetric {
	return workqueueLatency.WithLabelValues(name)
}

func (WorkqueueMetricsProvider) NewWorkDurationMetric(name string) workqueue.HistogramMetric {
	return workqueueDuration.WithLabelValues(name)
}

func (WorkqueueMetricsProvider) NewUnfinishedWorkSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return workqueueUnfinished.WithLabelValues(name)
}

func (WorkqueueMetricsProvider) NewLongestRunningProcessorSecondsMetric(name string) workqueue.SettableGaugeMetric {
	return workqueueLongestRunningProcessor.WithLabelValues(name)
}

func (WorkqueueMetricsProvider) NewRetriesMetric(name string) workqueue.CounterMetric {
	return workqueueRetries.WithLabelValues(name)
}
