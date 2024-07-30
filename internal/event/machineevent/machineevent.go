// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package machineevent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/gogo/protobuf/proto"
	irievent "github.com/ironcore-dev/ironcore/iri/apis/event/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"k8s.io/apimachinery/pkg/util/wait"
)

// EventStore represents an in-memory event store with TTL for events.
type EventStore struct {
	maxEvents int               // Maximum number of events in the store
	events    []*irievent.Event // Slice of events
	mutex     sync.Mutex        // Mutex for thread safety
	eventTTL  time.Duration     // TTL for events
	head      int               // Index of the oldest event
	count     int               // Current number of events in the store
	log       logr.Logger       // Logger for logging overridden events
}

// NewEventStore creates a new EventStore with a fixed number of events and set TTL for events.
func NewEventStore(log logr.Logger, maxEvents int, eventTTL time.Duration) *EventStore {
	return &EventStore{
		maxEvents: maxEvents,
		events:    make([]*irievent.Event, maxEvents),
		eventTTL:  eventTTL,
		head:      0,
		count:     0,
		log:       log,
	}
}

// AddEvent adds a new Event to the store.
func (es *EventStore) AddEvent(apiMetadata api.Metadata, eventType, reason, message string) error {
	es.mutex.Lock()
	defer es.mutex.Unlock()

	metadata, err := api.GetObjectMetadata(apiMetadata)
	if err != nil {
		return fmt.Errorf("error getting iri metadata: %w", err)
	}

	// Calculate the index where the new event will be inserted
	index := (es.head + es.count) % es.maxEvents

	// If the store is full, log and overwrite the oldest event and move the head
	if es.count == es.maxEvents {
		es.log.V(1).Info("Overriding event", "event", es.events[es.head])
		es.head = (es.head + 1) % es.maxEvents
	} else {
		es.count++
	}

	event := &irievent.Event{
		Spec: &irievent.EventSpec{
			InvolvedObjectMeta: metadata,
			Type:               eventType,
			Reason:             reason,
			Message:            message,
			EventTime:          time.Now().Unix(),
		},
	}

	es.events[index] = event
	return nil
}

// RemoveExpiredEvents checks and removes events whose TTL has expired.
func (es *EventStore) RemoveExpiredEvents() {
	es.mutex.Lock()
	defer es.mutex.Unlock()

	now := time.Now()

	for es.count > 0 {
		index := es.head % es.maxEvents
		event := es.events[index]
		eventTime := time.Unix(event.Spec.EventTime, 0)
		eventTimeWithDuration := eventTime.Add(es.eventTTL)

		if eventTimeWithDuration.After(now) {
			break
		}

		// Clear the reference to the expired event
		es.events[index] = nil
		es.head = (es.head + 1) % es.maxEvents
		es.count--
	}
}

// Start initializes and starts the event store's TTL expiration check.
func (es *EventStore) Start(ctx context.Context, setupLog logr.Logger, machineEventResyncInterval time.Duration) {
	defer func() {
		setupLog.Info("Shutting down machine events garbage collector")
	}()
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		es.RemoveExpiredEvents()
	}, machineEventResyncInterval)
}

// ListEvents returns a copy of all events currently in the store.
func (es *EventStore) ListEvents() []*irievent.Event {
	es.mutex.Lock()
	defer es.mutex.Unlock()

	result := make([]*irievent.Event, 0, es.count)
	for i := 0; i < es.count; i++ {
		index := (es.head + i) % es.maxEvents
		// Create a deep copy of the event to break the reference
		clone, ok := proto.Clone(es.events[index]).(*irievent.Event)
		if !ok {
			es.log.Error(fmt.Errorf("failed to clone event: %s", es.events[index]), "assertion error")
			continue
		}
		result = append(result, clone)
	}

	return result
}
