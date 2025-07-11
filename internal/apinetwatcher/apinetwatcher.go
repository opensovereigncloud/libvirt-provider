// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package apinetwatcher

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	apinetv1alpha1 "github.com/ironcore-dev/ironcore-net/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"
	"github.com/ironcore-dev/libvirt-provider/internal/event"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var ErrAPINetWatcherNotReady = errors.New("apinet watcher is not ready")

type Watcher interface {
	SetNodeName(string)
	SetAPINetConfig(*rest.Config)
	SetEventEmitter(EventEmitter[*apinetv1alpha1.NetworkInterface])
	Start(context.Context) error
	IsReady() bool
	SetDynamicClientOverride(dynamic.Interface)
}

type watcher struct {
	NodeName          string
	apinetCfg         *rest.Config
	emitter           EventEmitter[*apinetv1alpha1.NetworkInterface]
	ready             atomic.Bool
	dynClientOverride dynamic.Interface
	log               logr.Logger
}

// GVR used to access NetworkInterface resources via dynamic client
var nicGVR = schema.GroupVersionResource{
	Group:    "core.apinet.ironcore.dev",
	Version:  "v1alpha1",
	Resource: "networkinterfaces",
}

func NewWatcher(log logr.Logger) *watcher {
	return &watcher{log: log}
}

func (w *watcher) SetNodeName(name string) {
	w.NodeName = name
}

func (w *watcher) SetAPINetConfig(cfg *rest.Config) {
	w.apinetCfg = cfg
}

func (w *watcher) SetDynamicClientOverride(c dynamic.Interface) {
	w.dynClientOverride = c
}

func (w *watcher) SetEventEmitter(em EventEmitter[*apinetv1alpha1.NetworkInterface]) {
	w.emitter = em
}

// IsReady returns whether the watcher has successfully established a connection
// and is actively receiving events.
//
// This check should be used before creating or deleting NetworkInterface objects
// to avoid losing events that occur while the watcher is not yet connected.
// Once ready, the watcher is guaranteed to observe all future events.
//
// The watcher may not be ready during:
// - Initial startup (before first successful Watch)
// - API server unavailability
// - etcd compaction or flushing delays
// - Network partitions or DNS issues
// - Temporary authentication failures
func (w *watcher) IsReady() bool {
	return w.ready.Load()
}

func (w *watcher) Start(ctx context.Context) error {
	return w.run(ctx)
}

func (w *watcher) run(ctx context.Context) error {
	var dynClient dynamic.Interface
	var err error

	// Use test/mocked client if injected for testing, otherwise create one from config
	if w.dynClientOverride != nil {
		dynClient = w.dynClientOverride
	} else {
		dynClient, err = dynamic.NewForConfig(w.apinetCfg)
		if err != nil {
			return fmt.Errorf("failed to create dynamic client: %w", err)
		}
	}

	resourceClient := dynClient.Resource(nicGVR).Namespace(v1.NamespaceAll)
	labelSelector := fmt.Sprintf("%s=%s", api.LabelLibvirtProviderHostname, w.NodeName)

	reconnectDelay := time.Second

watchLoop:
	for {
		// Marks the watcher as not ready (e.g. during reconnects or watch failures)
		w.ready.Store(false)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Initial list to get current resourceVersion
		list, err := resourceClient.List(ctx, v1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			w.log.Error(err, "failed to list objects")
		}

		resourceVersion := ""
		if list != nil {
			resourceVersion = list.GetResourceVersion()
		}

		// Start the Watch stream from the latest resourceVersion
		watcher, err := resourceClient.Watch(ctx, v1.ListOptions{
			LabelSelector:   labelSelector,
			ResourceVersion: resourceVersion,
		})
		if err != nil {
			w.log.Error(err, "failed to start apinet NIC watcher")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(reconnectDelay):
				// Exponential backoff on watch failure
				reconnectDelay = minDuration(reconnectDelay*2, 3*time.Minute)
				continue
			}
		}

		w.log.V(1).Info("APINet NIC watcher connected")

		// Marks the watcher as ready after a successful Watch connection
		w.ready.Store(true)

		// Reset delay on success
		reconnectDelay = time.Second

		ch := watcher.ResultChan()
		for {
			select {
			case <-ctx.Done():
				watcher.Stop()
				return ctx.Err()
			case evt, ok := <-ch:
				if !ok {
					w.log.V(1).Info("Watch channel closed, reconnecting")
					watcher.Stop()
					continue watchLoop
				}

				unstructuredObj, ok := evt.Object.(runtime.Unstructured)
				if !ok {
					continue
				}

				obj := &apinetv1alpha1.NetworkInterface{}
				if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.UnstructuredContent(), obj); err != nil {
					w.log.Error(err, "failed to convert to NetworkInterface")
					continue
				}

				// Skip events for NICs that are not yet ready
				if obj.Status.State != apinetv1alpha1.NetworkInterfaceStateReady {
					w.log.V(2).Info("APINet NIC is not ready; skipping reconciliation", "name", obj.Name)
					continue
				}

				eventType := convertType(evt.Type)

				if w.emitter != nil {
					w.emitter.Fire(Event[*apinetv1alpha1.NetworkInterface]{
						Type:   string(eventType),
						Object: obj,
					})
				}
			}
		}
	}
}

// convertType converts raw Kubernetes watch event types to internal event types
func convertType(t watch.EventType) event.Type {
	switch t {
	case watch.Added:
		return event.TypeCreated
	case watch.Modified:
		return event.TypeUpdated
	case watch.Deleted:
		return event.TypeDeleted
	default:
		return event.TypeGeneric
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
