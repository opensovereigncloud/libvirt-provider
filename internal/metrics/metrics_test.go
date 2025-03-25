// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics_test

import (
	metrics "github.com/ironcore-dev/libvirt-provider/internal/metrics"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Prometheus Metric Fetching with Error Handling and No-Op Defaults", func() {
	Describe("GetCounterWithLabels", func() {
		It("should return a NoOpsMetricProvider and error for nil metric", func() {
			counter, err := metrics.GetCounterWithLabels(nil, testLabels)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(metrics.ErrNilMetric))
			Expect(counter).To(Equal(metrics.NoOpsMetricProvider))
		})

		It("should return a valid Counter when labels exist", func() {
			counter, err := metrics.GetCounterWithLabels(counterVec, testLabels)
			Expect(err).NotTo(HaveOccurred())
			Expect(counter).NotTo(BeNil())
			Expect(counter).NotTo(Equal(metrics.NoOpsMetricProvider))
		})

		It("should return NoOpsMetricProvider on missing label", func() {
			counter, err := metrics.GetCounterWithLabels(counterVec, invalidLabels)
			Expect(err).To(HaveOccurred())
			Expect(counter).To(Equal(metrics.NoOpsMetricProvider))
		})
	})

	Describe("GetGaugeWithLabels", func() {
		It("should return a NoOpsMetricProvider and error for nil metric", func() {
			counter, err := metrics.GetGaugeWithLabels(nil, testLabels)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(metrics.ErrNilMetric))
			Expect(counter).To(Equal(metrics.NoOpsMetricProvider))
		})

		It("should return a valid Gauge when labels exist", func() {
			gauge, err := metrics.GetGaugeWithLabels(gaugeVec, testLabels)
			Expect(err).NotTo(HaveOccurred())
			Expect(gauge).NotTo(BeNil())
			Expect(gauge).NotTo(Equal(metrics.NoOpsMetricProvider))
		})

		It("should return NoOpsMetricProvider on missing label", func() {
			gauge, err := metrics.GetGaugeWithLabels(gaugeVec, invalidLabels)
			Expect(err).To(HaveOccurred())
			Expect(gauge).To(Equal(metrics.NoOpsMetricProvider))
		})
	})

	Describe("GetHistogramWithLabels", func() {
		It("should return a NoOpsMetricProvider and error for nil metric", func() {
			counter, err := metrics.GetHistogramWithLabels(nil, testLabels)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(metrics.ErrNilMetric))
			Expect(counter).To(Equal(metrics.NoOpsMetricProvider))
		})

		It("should return a valid Histogram when labels exist", func() {
			histogram, err := metrics.GetHistogramWithLabels(histVec, testLabels)
			Expect(err).NotTo(HaveOccurred())
			Expect(histogram).NotTo(BeNil())
			Expect(histogram).NotTo(Equal(metrics.NoOpsMetricProvider))
		})

		It("should return NoOpsMetricProvider when labels do not match", func() {
			histogram, err := metrics.GetHistogramWithLabels(histVec, invalidLabels)
			Expect(err).To(HaveOccurred())
			Expect(histogram).To(Equal(metrics.NoOpsMetricProvider))
		})
	})

	Describe("GetSummaryWithLabels", func() {
		It("should return a NoOpsMetricProvider and error for nil metric", func() {
			counter, err := metrics.GetSummaryWithLabels(nil, testLabels)
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(metrics.ErrNilMetric))
			Expect(counter).To(Equal(metrics.NoOpsMetricProvider))
		})

		It("should return a valid Summary when labels exist", func() {
			summary, err := metrics.GetSummaryWithLabels(summaryVec, testLabels)
			Expect(err).NotTo(HaveOccurred())
			Expect(summary).NotTo(BeNil())
			Expect(summary).NotTo(Equal(metrics.NoOpsMetricProvider))
		})

		It("should return NoOpsMetricProvider when labels do not match", func() {
			summary, err := metrics.GetSummaryWithLabels(summaryVec, invalidLabels)
			Expect(err).To(HaveOccurred())
			Expect(summary).To(Equal(metrics.NoOpsMetricProvider))
		})
	})
})
