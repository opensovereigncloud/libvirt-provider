// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package metrics_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func TestUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics Suite")
}

var (
	counterVec *prometheus.CounterVec
	gaugeVec   *prometheus.GaugeVec
	histVec    *prometheus.HistogramVec
	summaryVec *prometheus.SummaryVec

	testLabels    = prometheus.Labels{labelTest: "value1"}
	invalidLabels = prometheus.Labels{"invalid_label": "value"}
	labelTest     = "labelTest"

	registry = prometheus.NewRegistry()
)

var _ = BeforeSuite(func() {
	counterVec = promauto.NewCounterVec(prometheus.CounterOpts{Name: "test_counter"}, []string{labelTest})
	gaugeVec = promauto.NewGaugeVec(prometheus.GaugeOpts{Name: "test_gauge"}, []string{labelTest})
	histVec = promauto.NewHistogramVec(prometheus.HistogramOpts{Name: "test_histogram"}, []string{labelTest})
	summaryVec = promauto.NewSummaryVec(prometheus.SummaryOpts{Name: "test_summary"}, []string{labelTest})

	collectors := []prometheus.Collector{counterVec, gaugeVec, histVec, summaryVec}
	for i := range collectors {
		err := registry.Register(collectors[i])
		Expect(err).NotTo(HaveOccurred())
	}
})
