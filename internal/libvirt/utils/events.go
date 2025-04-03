// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"
	"errors"
	"fmt"

	"github.com/digitalocean/go-libvirt"
	"github.com/go-logr/logr"
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/metrics"
	"github.com/ironcore-dev/libvirt-provider/internal/store"
	"github.com/ironcore-dev/libvirt-provider/internal/utils"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/util/workqueue"
)

const (
	DomainEventIDDomainEvent = "DomainEvent"

	DomainEventIDDeviceAdded    = "DeviceAdded"
	DomainEventIDDeviceRemoved  = "DeviceRemoved"
	DomainEventIDDiskChange     = "DiskChange"
	DomainEventIDLifecycle      = "Lifecycle"
	DomainEventIDMetadataChange = "MetadataChange"

	EventTypeUnknown = "unknown"

	LibvirtEvent = "libvirt-event"

	logMsgContextDone = "context done for libvirt event handler"

	logKeyEventHandler = "eventHandler"
)

var (
	// eventIDToLibvirtDomainLifecycleEvent maps domain lifecycle event IDs to their corresponding human-readable event types.
	// These IDs represent the various states that a domain can go through during its lifecycle.
	// Ref: https://libvirt.org/html/libvirt-libvirt-domain.html#virDomainEventType
	eventIDToLibvirtDomainLifecycleEvent = map[int32]string{
		0: "defined",
		1: "undefined",
		2: "started",
		3: "suspended",
		4: "resumed",
		5: "stopped",
		6: "shutdown",
		7: "pmsuspended",
		8: "crashed",
		9: "last",
	}

	ErrChannelClose = errors.New("event channel is closed")
)

func HandleEvents(ctx context.Context, log logr.Logger, clnt *libvirt.Libvirt, machineStore store.Store[*api.Machine], queue workqueue.TypedRateLimitingInterface[string]) error {
	mainChan := make(chan any, 10)
	defer close(mainChan)

	labels := prometheus.Labels{metrics.LabelOperation: LibvirtEvent}
	opsErrors, err := metrics.GetCounterWithLabels(metrics.OperationErrors, labels)
	if err != nil {
		return fmt.Errorf("failed to create operation errors metric for %s: %w", LibvirtEvent, err)
	}

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)
	defer cancel()

	g, childCTX := errgroup.WithContext(ctx)

	// https://pkg.go.dev/github.com/digitalocean/go-libvirt#DomainEventID
	supportedEvents := map[string]libvirt.DomainEventID{
		DomainEventIDDeviceAdded:    libvirt.DomainEventIDDeviceAdded,
		DomainEventIDDeviceRemoved:  libvirt.DomainEventIDDeviceRemoved,
		DomainEventIDDiskChange:     libvirt.DomainEventIDDiskChange,
		DomainEventIDLifecycle:      libvirt.DomainEventIDLifecycle,
		DomainEventIDMetadataChange: libvirt.DomainEventIDMetadataChange,
	}

	eventChannels := make(map[string]<-chan interface{}, len(supportedEvents))
	for name, eventID := range supportedEvents {
		log.V(1).Info("registration of libvirt event: " + name)
		eventChan, locErr := clnt.SubscribeEvents(childCTX, eventID, nil)
		if locErr != nil {
			opsErrors.Inc()
			return fmt.Errorf("failed to register event handler for event %s: %w", name, locErr)
		}

		eventChannels[name] = eventChan
	}

	for name, eventChan := range eventChannels {
		g.Go(func() error {
			for {
				select {
				case ev, ok := <-eventChan:
					if !ok {
						opsErrors.Inc()
						return fmt.Errorf("failed to get events from channel for %s: %w", name, ErrChannelClose)
					}
					mainChan <- ev
				case <-childCTX.Done():
					log.Info(logMsgContextDone, logKeyEventHandler, name)
					return nil
				}
			}
		})
	}

	g.Go(func() error {
		locLog := log.WithValues(logKeyEventHandler, "main")
		for {
			select {
			case ev, ok := <-mainChan:
				if !ok {
					return nil
				}
				locErr := processEvent(locLog, ev, machineStore, queue)
				if locErr != nil {
					opsErrors.Inc()
					locLog.Error(locErr, "failed to process event")
				}
			case <-childCTX.Done():
				locLog.Info(logMsgContextDone)
				return nil
			}
		}
	})

	return g.Wait()
}

func processEvent(log logr.Logger, event any, machineStore store.Store[*api.Machine], queue workqueue.TypedRateLimitingInterface[string]) error {
	defer utils.Recover(log, "libvirtutils.processEvent")

	var domainName, reason string
	var libvirtEventTotalMetric prometheus.Counter
	var metricsErr error
	var labels prometheus.Labels
	switch ev := event.(type) {
	case *libvirt.DomainEventCallbackDeviceAddedMsg:
		domainName = ev.Dom.Name
		reason = fmt.Sprintf("%T for device alias %s", ev, ev.DevAlias)
		labels = prometheus.Labels{"event_id": DomainEventIDDeviceAdded, "event_type": EventTypeUnknown}
		libvirtEventTotalMetric, metricsErr = metrics.GetCounterWithLabels(metrics.LibvirtEventsCount, labels)
	case *libvirt.DomainEventCallbackDeviceRemovedMsg:
		domainName = ev.Msg.Dom.Name
		reason = fmt.Sprintf("%T for device alias %s", ev, ev.Msg.DevAlias)
		labels = prometheus.Labels{"event_id": DomainEventIDDeviceRemoved, "event_type": EventTypeUnknown}
		libvirtEventTotalMetric, metricsErr = metrics.GetCounterWithLabels(metrics.LibvirtEventsCount, labels)
	case *libvirt.DomainEventCallbackDeviceRemovalFailedMsg:
		domainName = ev.Dom.Name
		reason = fmt.Sprintf("%T for device alias %s", ev, ev.DevAlias)
		labels = prometheus.Labels{"event_id": DomainEventIDDeviceRemoved, "event_type": EventTypeUnknown}
		libvirtEventTotalMetric, metricsErr = metrics.GetCounterWithLabels(metrics.LibvirtEventsCount, labels)
	case *libvirt.DomainEventCallbackDiskChangeMsg:
		domainName = ev.Msg.Dom.Name
		reason = fmt.Sprintf("%T for device alias %s with reason %d", ev, ev.Msg.DevAlias, ev.Msg.Reason)
		labels = prometheus.Labels{"event_id": DomainEventIDDiskChange, "event_type": EventTypeUnknown}
		libvirtEventTotalMetric, metricsErr = metrics.GetCounterWithLabels(metrics.LibvirtEventsCount, labels)
	case *libvirt.DomainEventCallbackLifecycleMsg:
		domainName = ev.Msg.Dom.Name
		reason = fmt.Sprintf("%T for event %d with detail %d", ev, ev.Msg.Event, ev.Msg.Detail)
		labels = prometheus.Labels{"event_id": DomainEventIDLifecycle, "event_type": GetLibvirtDomainLifecycleEvent(ev.Msg.Event)}
		libvirtEventTotalMetric, metricsErr = metrics.GetCounterWithLabels(metrics.LibvirtEventsCount, labels)
	case *libvirt.DomainEventCallbackMetadataChangeMsg:
		domainName = ev.Dom.Name
		reason = fmt.Sprintf("%T", ev)
		labels = prometheus.Labels{"event_id": DomainEventIDMetadataChange, "event_type": EventTypeUnknown}
		libvirtEventTotalMetric, metricsErr = metrics.GetCounterWithLabels(metrics.LibvirtEventsCount, labels)
	case *libvirt.DomainEvent:
		domainName = ev.Domain.Name
		reason = fmt.Sprintf("%T event %s with detail %s", ev, ev.Event, string(ev.Details))
		labels = prometheus.Labels{"event_id": DomainEventIDDomainEvent, "event_type": EventTypeUnknown}
		libvirtEventTotalMetric, metricsErr = metrics.GetCounterWithLabels(metrics.LibvirtEventsCount, labels)
	default:
		return fmt.Errorf("unknown event to process: %T", ev)
	}

	err := machineStore.Exists(domainName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			log.V(2).Info("Skipped: not managed by libvirt-provider", "machineID", domainName)
			return nil
		}
		return fmt.Errorf("failed to check existence of machine %s: %w", domainName, err)
	}

	if metricsErr != nil {
		log.Error(metricsErr, "failed to get metric for event", metrics.LogKeyLabels, labels)
	}

	libvirtEventTotalMetric.Inc()

	log.V(1).Info("requeue machine by event message: "+reason, "machineID", domainName)
	queue.AddRateLimited(domainName)

	return nil
}

func GetLibvirtDomainLifecycleEvent(id int32) string {
	eventType, exists := eventIDToLibvirtDomainLifecycleEvent[id]
	if !exists {
		eventType = EventTypeUnknown
	}

	return eventType
}
