// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package apinetwatcher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	apinetv1alpha1 "github.com/ironcore-dev/ironcore-net/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/internal/event"
	internalutils "github.com/ironcore-dev/libvirt-provider/internal/utils"

	"k8s.io/apimachinery/pkg/api/equality"
	clientgoCache "k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

var (
	ErrCacheSync = fmt.Errorf("failed to sync cache")
)

type Watcher interface {
	// Start begins the informer cache event loop for the watcher.
	//
	// The underlying informer cache runs in its own goroutines, where it performs
	// LIST/WATCH requests against the API server to maintain an up-to-date local
	// cache of the watched resources. Calling Start is what actually launches this
	// machinery. Without it, the cache would remain idle and no events would flow.
	//
	// This method also wires up the provided EventEmitter, which the watcher uses
	// later to forward resource events (create/update/delete) to the controller’s
	// reconciliation logic.
	//
	// Start will block until the context is canceled or an unrecoverable error occurs.
	Start(context.Context, EventEmitter[*apinetv1alpha1.NetworkInterface]) error

	// WaitForCacheSync blocks until the cache has successfully observed the initial
	// state of all watched resources or until the provided context times out/cancels.
	// This guarantees that the controller has a consistent view of cluster state
	// before processing any events. Without this step, the controller could attempt
	// to act on incomplete data, leading to subtle race conditions and hard-to-debug errors.
	WaitForCacheSync(context.Context) error
}

type watcher struct {
	emitter          EventEmitter[*apinetv1alpha1.NetworkInterface]
	log              logr.Logger
	cache            cache.Cache
	cacheSyncTimeout time.Duration
}

type relaventNICStatus struct {
	State      apinetv1alpha1.NetworkInterfaceState
	PCIAddress apinetv1alpha1.PCIAddress
	TAPDevice  apinetv1alpha1.TAPDevice
}

func NewWatcher(ctx context.Context, cache cache.Cache, timeout time.Duration) (*watcher, error) {
	w := &watcher{
		log:              ctrl.Log.WithName("apinet-nic-watcher"),
		cache:            cache,
		cacheSyncTimeout: timeout,
	}
	informer, err := w.cache.GetInformer(ctx, &apinetv1alpha1.NetworkInterface{})
	if err != nil {
		return nil, fmt.Errorf("failed to construct informer: %w", err)
	}

	_, err = informer.AddEventHandler(clientgoCache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			// Skipping NIC Add events intentionally.
			// Reason:
			// 1. libvirt-provider is the one creating these objects, so reconciling on creation
			//    would just cause us to react to our own writes. The objects only become meaningful
			//    after they are updated by the relevant controllers (e.g. with State, PCIAddress, etc.).
			// 2. When the application starts, the informer replays all existing objects as "add" events.
			//    Handling those would trigger a storm of unnecessary reconciliations at startup.
		},
		UpdateFunc: func(oldObj, newObj any) {
			oldNIC, okOld := oldObj.(*apinetv1alpha1.NetworkInterface)
			newNIC, okNew := newObj.(*apinetv1alpha1.NetworkInterface)
			if !okOld || !okNew {
				return
			}
			w.handleNICUpdate(oldNIC, newNIC)
		},
		DeleteFunc: func(obj any) {
			nic, ok := obj.(*apinetv1alpha1.NetworkInterface)
			if ok {
				w.emitAPINetNICEvent(event.TypeDeleted, nic)
			}
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add event handler to informer: %w", err)
	}
	return w, nil
}

func (w *watcher) Start(ctx context.Context, em EventEmitter[*apinetv1alpha1.NetworkInterface]) error {
	w.emitter = em

	err := w.cache.Start(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("cache.Start failed: %w", err)
	}

	return nil
}

func (w *watcher) handleNICUpdate(oldNIC, newNIC *apinetv1alpha1.NetworkInterface) {
	nicName := newNIC.Name

	oldStatus := relaventNICStatus{
		State: oldNIC.Status.State,
	}
	if oldNIC.Status.PCIAddress != nil {
		oldStatus.PCIAddress = *oldNIC.Status.PCIAddress
	}
	if oldNIC.Status.TAPDevice != nil {
		oldStatus.TAPDevice = *oldNIC.Status.TAPDevice
	}

	newStatus := relaventNICStatus{
		State: newNIC.Status.State,
	}
	if newNIC.Status.PCIAddress != nil {
		newStatus.PCIAddress = *newNIC.Status.PCIAddress
	}
	if newNIC.Status.TAPDevice != nil {
		newStatus.TAPDevice = *newNIC.Status.TAPDevice
	}

	// Skip if no meaningful change
	if equality.Semantic.DeepEqual(oldStatus, newStatus) {
		w.log.V(2).Info("No meaningful NIC change; skipping", internalutils.LogKeyNICName, nicName)
		return
	}

	w.log.V(1).Info("NIC updated", internalutils.LogKeyNICName, nicName)
	w.emitAPINetNICEvent(event.TypeUpdated, newNIC)
}

func (w *watcher) emitAPINetNICEvent(eventType event.Type, obj *apinetv1alpha1.NetworkInterface) {
	if w.emitter != nil {
		w.emitter.Fire(Event[*apinetv1alpha1.NetworkInterface]{
			Type:   string(eventType),
			Object: obj,
		})
	}
}

func (w *watcher) WaitForCacheSync(ctx context.Context) error {
	w.log.Info("Waiting for cache synchronization.")

	locCtx, locCtxCancel := context.WithTimeout(ctx, w.cacheSyncTimeout)
	defer locCtxCancel()

	if !w.cache.WaitForCacheSync(locCtx) {
		return locCtx.Err()
	}

	w.log.Info("Cache synchronization completed.")
	return nil
}
