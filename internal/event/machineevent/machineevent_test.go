// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package machineevent_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ironcore-dev/libvirt-provider/api"

	"github.com/go-logr/logr/funcr"
	. "github.com/ironcore-dev/libvirt-provider/internal/event/machineevent"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Machine Event Suite")
}

const (
	maxEvents      = 5
	eventTTL       = 2 * time.Second
	eventType      = "TestType"
	reason         = "TestReason"
	message        = "TestMessage"
	resyncInterval = 2 * time.Second
)

var (
	es          *EventStore
	apiMetadata = api.Metadata{
		ID: "test-id-1234",
		Annotations: map[string]string{
			"libvirt-provider.ironcore.dev/annotations": "{\"key1\":\"value1\", \"key2\":\"value2\"}",
			"libvirt-provider.ironcore.dev/labels":      "{\"downward-api.machinepoollet.ironcore.dev/root-machine-namespace\":\"default\", \"downward-api.machinepoollet.ironcore.dev/root-machine-name\":\"machine1\"}",
		}}
	log  = funcr.New(func(prefix, args string) {}, funcr.Options{})
	opts = EventStoreOptions{
		MachineEventMaxEvents:      maxEvents,
		MachineEventTTL:            eventTTL,
		MachineEventResyncInterval: resyncInterval,
	}
)

var _ = Describe("Machine EventStore", func() {
	BeforeEach(func() {
		es = NewEventStore(log, opts)
	})

	Context("Initialization", func() {
		It("should initialize events slice with no elements", func() {
			Expect(es.ListEvents()).To(BeEmpty())
		})
	})

	Context("AddEvent", func() {
		It("should add an event to the store", func() {
			err := es.Eventf(apiMetadata, eventType, reason, message)
			Expect(err).ToNot(HaveOccurred())
			Expect(es.ListEvents()).To(HaveLen(1))
		})

		It("should handle error when retrieving metadata", func() {
			badMetadata := api.Metadata{
				ID: "test-id-1234"}
			err := es.Eventf(badMetadata, eventType, reason, message)
			Expect(err).To(HaveOccurred())
			Expect(es.ListEvents()).To(HaveLen(0))
		})

		It("should override the oldest event when the store is full", func() {
			for i := 0; i < maxEvents; i++ {
				err := es.Eventf(apiMetadata, eventType, reason, fmt.Sprintf("%s %d", message, i))
				Expect(err).ToNot(HaveOccurred())
				Expect(es.ListEvents()).To(HaveLen(i + 1))
			}

			err := es.Eventf(apiMetadata, eventType, reason, "New Event")
			Expect(err).ToNot(HaveOccurred())

			events := es.ListEvents()
			Expect(events).To(HaveLen(maxEvents))

			for i := 0; i < maxEvents-1; i++ {
				Expect(events[i].Spec.Message).To(Equal(fmt.Sprintf("%s %d", message, i+1)))
			}

			Expect(events[maxEvents-1].Spec.Message).To(Equal("New Event"))
		})
	})

	Context("removeExpiredEvents", func() {
		It("should remove events whose TTL has expired", func() {
			err := es.Eventf(apiMetadata, eventType, reason, message)
			Expect(err).ToNot(HaveOccurred())
			Expect(es.ListEvents()).To(HaveLen(1))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go es.Start(ctx)

			Eventually(func(g Gomega) bool {
				return g.Expect(es.ListEvents()).To(HaveLen(0))
			}).WithTimeout(eventTTL + 1*time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())
		})

		It("should not remove events whose TTL has not expired", func() {
			err := es.Eventf(apiMetadata, eventType, reason, message)
			Expect(err).ToNot(HaveOccurred())
			Expect(es.ListEvents()).To(HaveLen(1))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go es.Start(ctx)

			Expect(es.ListEvents()).To(HaveLen(1))
		})
	})

	Context("Start", func() {
		It("should periodically remove expired events", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go es.Start(ctx)

			err := es.Eventf(apiMetadata, eventType, reason, message)
			Expect(err).ToNot(HaveOccurred())
			Expect(es.ListEvents()).To(HaveLen(1))

			Eventually(func(g Gomega) bool {
				return g.Expect(es.ListEvents()).To(HaveLen(0))
			}).WithTimeout(resyncInterval + 1*time.Second).WithPolling(100 * time.Millisecond).Should(BeTrue())
		})
	})

	Context("ListEvents", func() {
		It("should return all current events", func() {
			err := es.Eventf(apiMetadata, eventType, reason, message)
			Expect(err).ToNot(HaveOccurred())

			events := es.ListEvents()
			Expect(events).To(HaveLen(1))
			Expect(events[0].Spec.Message).To(Equal(message))
		})

		It("should return a copy of events", func() {
			err := es.Eventf(apiMetadata, eventType, reason, message)
			Expect(err).ToNot(HaveOccurred())
			events := es.ListEvents()
			Expect(events).To(HaveLen(1))

			events[0].Spec.Message = "Changed Message"

			storedEvents := es.ListEvents()
			Expect(storedEvents[0].Spec.Message).ToNot(Equal(events[0].Spec.Message))
		})
	})
})
