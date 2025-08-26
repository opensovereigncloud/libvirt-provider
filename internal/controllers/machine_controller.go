// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"time"

	"github.com/digitalocean/go-libvirt"
	"github.com/go-logr/logr"
	apinetv1alpha1 "github.com/ironcore-dev/ironcore-net/api/core/v1alpha1"
	core "github.com/ironcore-dev/ironcore/api/core/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"

	"github.com/ironcore-dev/libvirt-provider/internal/apinetwatcher"
	"github.com/ironcore-dev/libvirt-provider/internal/event"
	machineEvent "github.com/ironcore-dev/libvirt-provider/internal/event/machineevent"
	providerhost "github.com/ironcore-dev/libvirt-provider/internal/host"
	"github.com/ironcore-dev/libvirt-provider/internal/libvirt/guest"
	libvirtmeta "github.com/ironcore-dev/libvirt-provider/internal/libvirt/meta"
	libvirtutils "github.com/ironcore-dev/libvirt-provider/internal/libvirt/utils"
	providerlibvirtxml "github.com/ironcore-dev/libvirt-provider/internal/libvirtxml"
	"github.com/ironcore-dev/libvirt-provider/internal/metrics"
	"github.com/ironcore-dev/libvirt-provider/internal/networkinterfaceplugin"
	providerimage "github.com/ironcore-dev/libvirt-provider/internal/oci"
	"github.com/ironcore-dev/libvirt-provider/internal/osutils"
	providernetworkinterface "github.com/ironcore-dev/libvirt-provider/internal/plugins/networkinterface"
	"github.com/ironcore-dev/libvirt-provider/internal/plugins/networkinterface/apinet"
	providervolume "github.com/ironcore-dev/libvirt-provider/internal/plugins/volume"
	"github.com/ironcore-dev/libvirt-provider/internal/raw"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/manager"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/sources"
	"github.com/ironcore-dev/libvirt-provider/internal/sgx"
	"github.com/ironcore-dev/libvirt-provider/internal/store"
	internalutils "github.com/ironcore-dev/libvirt-provider/internal/utils"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	"libvirt.org/go/libvirtxml"
)

const (
	MachineFinalizer = "machine"
	permFile         = 0660
	// libvirt daemon can overtake ownership
	permFileIgnition                = 0660
	rootFSAlias                     = "ua-rootfs"
	libvirtDomainXMLIgnitionKeyName = "opt/com.coreos/config"
	networkInterfaceAliasPrefix     = "ua-networkinterface-"

	MachineReconcilerName                = "machine-reconciler"
	MachineReconcilerOpsVolumeSize       = "volume-size"
	MachineReconcilerOpsGarbageCollector = "garbage-collector"
	MachineReconcilerMetrics             = "metrics"

	ArchitectureAARCH64 = "aarch64"
	ArchitectureX8664   = "x86_64"
)

var (
	// TODO: improve domainStateToMachineState since some states are mapped to computev1alpha1.MachineStatePending
	// where it doesn't make that much sense.
	domainStateToMachineState = map[libvirt.DomainState]api.MachineState{
		libvirt.DomainNostate:  api.MachineStatePending,
		libvirt.DomainRunning:  api.MachineStateRunning,
		libvirt.DomainBlocked:  api.MachineStatePending,
		libvirt.DomainPaused:   api.MachineStatePending,
		libvirt.DomainShutdown: api.MachineStateTerminating,
		// it isn't probably supported by transient domain
		libvirt.DomainShutoff:     api.MachineStateTerminated,
		libvirt.DomainPmsuspended: api.MachineStatePending,
	}
)

type MachineReconcilerOptions struct {
	GuestCapabilities              guest.Capabilities
	ImageCache                     providerimage.Cache
	Raw                            raw.Raw
	VolumePluginManager            *providervolume.PluginManager
	NetworkInterfacePlugin         providernetworkinterface.Plugin
	VolumeEvents                   event.Source[*api.Machine]
	ResyncIntervalGarbageCollector time.Duration
	GCVMGracefulShutdownTimeout    time.Duration
	VolumeCachePolicyCeph          string
	OverrideDomainXML              *libvirtxml.Domain
	Queue                          workqueue.TypedRateLimitingInterface[string]
	WatcherEmitter                 apinetwatcher.EventEmitter[*apinetv1alpha1.NetworkInterface]
}

func NewMachineReconciler(
	log logr.Logger,
	host providerhost.LibvirtHost,
	machines store.Store[*api.Machine],
	machineEvents event.Source[*api.Machine],
	eventRecorder machineEvent.EventRecorder,
	opts MachineReconcilerOptions,
) (*MachineReconciler, error) {
	if host == nil {
		return nil, fmt.Errorf("must specify libvirt host")
	}

	if machines == nil {
		return nil, fmt.Errorf("must specify machine store")
	}

	if machineEvents == nil {
		return nil, fmt.Errorf("must specify machine events")
	}

	labels := prometheus.Labels{metrics.LabelController: MachineReconcilerName}
	durationSummary, err := metrics.GetSummaryWithLabels(metrics.ControllerRuntimeReconcileDuration, labels)
	if err != nil {
		log.Error(err, "failed to get reconcile duration metric", metrics.LogKeyLabels, labels)
	}

	activeWorkerGauge, err := metrics.GetGaugeWithLabels(metrics.ControllerRuntimeActiveWorker, labels)
	if err != nil {
		log.Error(err, "failed to get active workers metric", metrics.LogKeyLabels, labels)
	}

	reconcileErrorsCounter, err := metrics.GetCounterWithLabels(metrics.ControllerRuntimeReconcileErrors, labels)
	if err != nil {
		return nil, fmt.Errorf("failed to get reconcile errors total metric: %w", err)
	}

	return &MachineReconciler{
		log:                                     log,
		queue:                                   opts.Queue,
		host:                                    host,
		machines:                                machines,
		machineEvents:                           machineEvents,
		EventRecorder:                           eventRecorder,
		guestCapabilities:                       opts.GuestCapabilities,
		imageCache:                              opts.ImageCache,
		raw:                                     opts.Raw,
		volumePluginManager:                     opts.VolumePluginManager,
		networkInterfacePlugin:                  opts.NetworkInterfacePlugin,
		resyncIntervalGarbageCollector:          opts.ResyncIntervalGarbageCollector,
		gcVMGracefulShutdownTimeout:             opts.GCVMGracefulShutdownTimeout,
		volumeCachePolicyCeph:                   opts.VolumeCachePolicyCeph,
		overrideDomainXML:                       opts.OverrideDomainXML,
		metricsReconcileDuration:                durationSummary,
		metricsControllerRuntimeActiveWorker:    activeWorkerGauge,
		metricsControllerRuntimeReconcileErrors: reconcileErrorsCounter,
		watcherEmitter:                          opts.WatcherEmitter,
	}, nil
}

type MachineReconciler struct {
	log   logr.Logger
	queue workqueue.TypedRateLimitingInterface[string]

	guestCapabilities guest.Capabilities
	host              providerhost.LibvirtHost
	imageCache        providerimage.Cache
	raw               raw.Raw

	volumePluginManager    *providervolume.PluginManager
	networkInterfacePlugin providernetworkinterface.Plugin

	machines      store.Store[*api.Machine]
	machineEvents event.Source[*api.Machine]
	machineEvent.EventRecorder

	gcVMGracefulShutdownTimeout    time.Duration
	resyncIntervalGarbageCollector time.Duration

	volumeCachePolicyCeph string

	metricsReconcileDuration                prometheus.Observer
	metricsControllerRuntimeActiveWorker    prometheus.Gauge
	metricsControllerRuntimeReconcileErrors prometheus.Counter

	overrideDomainXML *libvirtxml.Domain

	watcherEmitter apinetwatcher.EventEmitter[*apinetv1alpha1.NetworkInterface]
}

func (r *MachineReconciler) Start(ctx context.Context) error {
	log := r.log

	//todo make configurable
	workerSize := int64(15)

	labels := prometheus.Labels{metrics.LabelController: MachineReconcilerName}
	maxConcurrentReconcilesGauge, err := metrics.GetGaugeWithLabels(metrics.ControllerRuntimeMaxConcurrentReconciles, labels)
	if err != nil {
		log.Error(err, "failed to get max concurrent reconciles metric", metrics.LogKeyLabels, labels)
	}
	maxConcurrentReconcilesGauge.Set(float64(workerSize))

	r.imageCache.AddListener(providerimage.ListenerFuncs{
		HandlePullDoneFunc: func(evt providerimage.PullDoneEvent) {
			machines, err := r.machines.List(ctx)
			if err != nil {
				log.Error(err, "failed to list machine")
				return
			}

			for _, machine := range machines {
				if ptr.Deref(machine.Spec.Image, "") == evt.Ref {
					r.Eventf(log, machine.Metadata, corev1.EventTypeNormal, "PulledImage", "Pulled image %s", *machine.Spec.Image)
					log.V(1).Info("Image pulled: Requeue machines", "Image", evt.Ref, "Machine", machine.ID)
					r.queue.Add(machine.ID)
				}
			}
		},
	})

	if r.networkInterfacePlugin.Name() == networkinterfaceplugin.PluginAPINet {
		err := r.watcherEmitter.AddHandler("watcher-apinet-nic", apinetwatcher.HandlerFunc[*apinetv1alpha1.NetworkInterface](func(evt apinetwatcher.Event[*apinetv1alpha1.NetworkInterface]) {
			nic := evt.Object
			if nic == nil {
				return
			}

			machines, err := r.machines.List(ctx)
			if err != nil {
				r.log.Error(err, "failed to list machines")
				return
			}

			for _, machine := range machines {
				for _, iface := range machine.Spec.NetworkInterfaces {
					machineID := machine.ID
					computed := apinet.NICName(machineID, iface.Name)
					if computed == nic.Name {
						r.log.V(1).Info("Requeuing machine due to apinet NIC event", internalutils.LogKeyMachineID, machineID, "NICName", computed, "eventType", evt.Type)
						r.queue.Add(machineID)
					}
				}
			}
		}))
		if err != nil {
			r.log.Error(err, "failed to register apinet NIC event handler")
		}
	}

	imgEventReg, err := r.machineEvents.AddHandler(event.HandlerFunc[*api.Machine](func(evt event.Event[*api.Machine]) {
		r.queue.Add(evt.Object.ID)
	}))
	if err != nil {
		return err
	}
	defer func() {
		if err = r.machineEvents.RemoveHandler(imgEventReg); err != nil {
			log.Error(err, "failed to remove machine event handler")
		}
	}()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return r.startGarbageCollector(ctx)
	})

	g.Go(func() error {
		<-ctx.Done()
		log.Info("Shutting down work queue")
		r.queue.ShutDown()
		return nil
	})

	for i := int64(0); i < workerSize; i++ {
		g.Go(func() error {
			log.V(1).Info("starting worker: " + strconv.FormatInt(i, 10))
			for r.processNextWorkItem(ctx, log) {
			}
			return nil
		})
	}

	return g.Wait()
}

func (r *MachineReconciler) startGarbageCollector(ctx context.Context) error {
	log := r.log.WithName(MachineReconcilerOpsGarbageCollector)

	labels := prometheus.Labels{metrics.LabelOperation: MachineReconcilerOpsGarbageCollector}
	opsDuration, err := metrics.GetSummaryWithLabels(metrics.OperationDuration, labels)
	if err != nil {
		r.log.Error(err, "failed to get operation duration metric", metrics.LogKeyLabels, labels)
	}

	opsErrors, err := metrics.GetCounterWithLabels(metrics.OperationErrors, labels)
	if err != nil {
		return fmt.Errorf("failed to get operation errors metric from startGarbageCollector: %w", err)
	}

	wait.UntilWithContext(ctx, func(ctx context.Context) {
		log.V(1).Info("starting garbage-collector loop")
		startTime := time.Now()
		defer func() {
			opsDuration.Observe(float64(time.Since(startTime).Milliseconds()) / 1000)
		}()

		defer internalutils.Recover(r.log, "startGarbageCollector")

		machines, err := r.machines.List(ctx)
		if err != nil {
			opsErrors.Inc()
			log.Error(err, "failed to list machines")
			return
		}

		for _, machine := range machines {
			if !slices.Contains(machine.Finalizers, MachineFinalizer) || machine.DeletedAt == nil {
				continue
			}

			logger := log.WithValues(internalutils.LogKeyMachineID, machine.ID)
			if err := r.processMachineDeletion(ctx, logger, machine); err != nil {
				opsErrors.Inc()
				logger.Error(err, "failed to garbage collect machine")
			}

		}

	}, r.resyncIntervalGarbageCollector)

	return nil
}

func (r *MachineReconciler) processMachineDeletion(ctx context.Context, log logr.Logger, machine *api.Machine) error {
	isDeleting, err := r.deleteMachine(ctx, log, machine)
	switch {
	case isDeleting:
		return nil
	case err != nil:
		return fmt.Errorf("failed to delete machine: %w", err)
	}
	log.V(1).Info("Deleted machine")

	machine.Status.State = api.MachineStateTerminated
	machine, err = r.machines.Update(ctx, machine)
	if err != nil {
		return fmt.Errorf("failed to update machine state: %w", err)
	}

	if err := r.deleteVolumes(ctx, log, machine); err != nil {
		return fmt.Errorf("failed to remove machine disks: %w", err)
	}
	log.V(1).Info("Removed machine disks")

	if err := r.deleteNetworkInterfaces(ctx, log, machine); err != nil {
		return fmt.Errorf("failed to remove machine network interfaces: %w", err)
	}
	log.V(1).Info("Removed network interfaces")

	if err := os.RemoveAll(r.host.MachineDir(machine.ID)); err != nil {
		return fmt.Errorf("failed to remove machine directory: %w", err)
	}
	log.V(1).Info("Removed machine directory")

	err = manager.Deallocate(machine, machine.Spec.Resources.DeepCopy())
	if err != nil {
		return fmt.Errorf("failed to deallocate resources: %w", err)
	}
	log.V(1).Info("Resources were deallocated")

	machine.Finalizers = internalutils.DeleteSliceElement(machine.Finalizers, MachineFinalizer)
	if _, err := r.machines.Update(ctx, machine); store.IgnoreErrNotFound(err) != nil {
		return fmt.Errorf("failed to update machine metadata: %w", err)
	}

	r.Eventf(log, machine.Metadata, corev1.EventTypeNormal, "CompletedDeletion", "Deletion completed")
	log.V(1).Info("Removed Finalizer. Deletion completed")

	return nil
}

func (r *MachineReconciler) deleteMachine(ctx context.Context, log logr.Logger, machine *api.Machine) (bool, error) {
	domain := libvirt.Domain{
		UUID: libvirtutils.UUIDStringToBytes(machine.ID),
	}

	if machine.Spec.ShutdownAt.IsZero() {
		machine.Status.State = api.MachineStateTerminating
		machine.Spec.ShutdownAt = time.Now()
		if _, err := r.machines.Update(ctx, machine); err != nil {
			return false, fmt.Errorf("failed to update ShutdownAt and State: %w", err)
		}

		log.V(1).Info("Updated ShutdownAt and State", "ShutdownAt", machine.Spec.ShutdownAt, "State", machine.Status.State)
	}

	if time.Now().Before(machine.Spec.ShutdownAt.Add(r.gcVMGracefulShutdownTimeout)) {
		// Due to heavy load, the AcpiPowerBtn signal might be missed by the VM.
		// Hence, triggering the machine shutdown until VMGracefulShutdownTimeout is over to ensure its reception.
		return r.shutdownMachine(log, machine, domain)
	}

	return false, r.destroyDomain(log, machine, domain)
}

func (r *MachineReconciler) destroyDomain(log logr.Logger, machine *api.Machine, domain libvirt.Domain) error {
	// DomainDestroyFlags is a blocking operation, and its synchronous nature may pose potential performance issues in the future.
	// During test involving 26 empty disks, the function call took a maximum of 1 second to complete.
	if err := r.host.Libvirt().DomainDestroyFlags(domain, libvirt.DomainDestroyGraceful); err != nil {
		if libvirt.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to initiate forceful shutdown: %w", err)
	}

	r.Eventf(log, machine.Metadata, corev1.EventTypeWarning, "DestroyedDomain", "Domain Destroyed")

	log.V(1).Info("Destroyed domain")
	metrics.MachinesDestroyed.Inc()
	return nil
}

func (r *MachineReconciler) shutdownMachine(log logr.Logger, machine *api.Machine, domain libvirt.Domain) (bool, error) {
	log.V(1).Info("Triggering shutdown", "ShutdownAt", machine.Spec.ShutdownAt)
	r.Eventf(log, machine.Metadata, corev1.EventTypeNormal, "TriggeringShutdown", "Shutdown Triggered")

	shutdownMode := libvirt.DomainShutdownAcpiPowerBtn
	if machine.Spec.GuestAgent == api.GuestAgentQemu {
		shutdownMode = libvirt.DomainShutdownGuestAgent
	}
	if err := r.host.Libvirt().DomainShutdownFlags(domain, shutdownMode); err != nil {
		if libvirt.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to initiate shutdown: %w", err)
	}

	return true, nil
}

func (r *MachineReconciler) processNextWorkItem(ctx context.Context, log logr.Logger) bool {
	defer internalutils.Recover(r.log, "processNextWorkItem")

	id, shutdown := r.queue.Get()
	if shutdown {
		return false
	}

	r.metricsControllerRuntimeActiveWorker.Inc()

	defer r.queue.Done(id)
	defer func() {
		r.metricsControllerRuntimeActiveWorker.Dec()
	}()

	reconcileID, err := internalutils.GenerateUUIDv7()
	if err != nil {
		log.Error(err, "failed to generate reconcile ID")
	}
	log = log.WithValues(internalutils.LogKeyMachineID, id, "reconcileID", reconcileID)
	ctx = logr.NewContext(ctx, log)

	startTime := time.Now()
	defer func() {
		r.metricsReconcileDuration.Observe(float64(time.Since(startTime).Milliseconds()) / 1000)
	}()

	if err := r.reconcileMachine(ctx, id); err != nil {
		log.Error(err, "failed to reconcile machine")
		r.queue.AddRateLimited(id)
		r.metricsControllerRuntimeReconcileErrors.Inc()
		return true
	}

	r.queue.Forget(id)
	return true
}

func (r *MachineReconciler) reconcileMachine(ctx context.Context, id string) error {
	log := logr.FromContextOrDiscard(ctx)

	log.V(2).Info("Getting machine from store", "id", id)
	machine, err := r.machines.Get(ctx, id)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("failed to fetch machine from store: %w", err)
		}

		return nil
	}

	if machine.DeletedAt != nil {
		return nil
	}

	if !slices.Contains(machine.Finalizers, MachineFinalizer) {
		machine.Finalizers = append(machine.Finalizers, MachineFinalizer)
		if _, err := r.machines.Update(ctx, machine); err != nil {
			return fmt.Errorf("failed to set finalizers: %w", err)
		}
		return nil
	}

	log.V(1).Info("Making machine directories")
	if err := providerhost.MakeMachineDirs(r.host, machine.ID); err != nil {
		return fmt.Errorf("error making machine directories: %w", err)
	}
	log.V(1).Info("Successfully made machine directories")

	log.V(1).Info("Reconciling domain")
	state, volumeStates, nicStates, err := r.reconcileDomain(ctx, log, machine)
	if err != nil {
		err = providerimage.IgnoreImagePulling(err)
		locErr := r.updateAPIMachineStatus(ctx, machine, state, volumeStates, nicStates)
		if locErr != nil {
			if err == nil {
				return fmt.Errorf("failed to update API machine: %w", locErr)
			}
			log.Error(locErr, "failed to update API machine")
		}
		return err
	}
	log.V(1).Info("Reconciled domain")

	err = r.updateAPIMachineStatus(ctx, machine, state, volumeStates, nicStates)
	if err != nil {
		return fmt.Errorf("failed to update machine status: %w", err)
	}

	return nil
}

func (r *MachineReconciler) reconcileDomain(
	ctx context.Context,
	log logr.Logger,
	machine *api.Machine,
) (api.MachineState, []api.VolumeStatus, []api.NetworkInterfaceStatus, error) {
	log.V(1).Info("Looking up domain")
	if _, err := r.host.Libvirt().DomainLookupByUUID(libvirtutils.UUIDStringToBytes(machine.ID)); err != nil {
		if !libvirt.IsNotFound(err) {
			return "", nil, nil, fmt.Errorf("error getting domain %s: %w", machine.ID, err)
		}

		log.V(1).Info("Creating new domain")
		volumeStates, nicStates, err := r.createDomain(ctx, log, machine)
		if err != nil {
			for i := range volumeStates {
				volumeStates[i].State = api.VolumeStatePending
			}
			for i := range nicStates {
				nicStates[i].State = api.NetworkInterfaceStatePending
			}
			return api.MachineStatePending, volumeStates, nicStates, err
		}

		log.V(1).Info("Created domain")
		return api.MachineStatePending, volumeStates, nicStates, nil
	}

	log.V(1).Info("Updating existing domain")
	volumeStates, nicStates, updateDomainErr := r.updateDomain(ctx, log, machine)

	// Always attempt to get the machine state, even if updateDomain fails.
	state, stateErr := r.getMachineState(machine.ID)
	if stateErr != nil {
		errMsg := fmt.Errorf("error getting machine state: %w", stateErr)
		if updateDomainErr != nil {
			errMsg = fmt.Errorf("updateDomain failed: %w; also %w", updateDomainErr, stateErr)
		}
		return "", volumeStates, nicStates, errMsg
	}

	return state, volumeStates, nicStates, updateDomainErr
}

func (r *MachineReconciler) updateDomain(
	ctx context.Context,
	log logr.Logger,
	machine *api.Machine,
) ([]api.VolumeStatus, []api.NetworkInterfaceStatus, error) {
	domainDesc, err := r.getDomainDesc(machine.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("error getting domain description: %w", err)
	}

	attacher, err := NewLibvirtVolumeAttacher(domainDesc, NewRunningDomainExecutor(r.host.Libvirt(), machine.ID), r.volumeCachePolicyCeph)
	if err != nil {
		return nil, nil, fmt.Errorf("error construction volume attacher: %w", err)
	}

	volumeStates, err := r.reconcileVolumes(ctx, log, machine, attacher)
	if err != nil {
		r.Eventf(log, machine.Metadata, corev1.EventTypeWarning, "reconcileVolumes", "Volume reconciliation failed with error: %s", err)
		return volumeStates, nil, fmt.Errorf("[volumes] %w", err)
	}

	nicStates, err := r.reconcileNetworkInterfaces(ctx, log, machine, domainDesc)
	if err != nil {
		r.Eventf(log, machine.Metadata, corev1.EventTypeWarning, "reconcileNetworkInterfaces", "NIC reconciliation failed with error: %s", err)
		return volumeStates, nicStates, fmt.Errorf("[network interfaces] %w", err)
	}

	return volumeStates, nicStates, nil
}

func (r *MachineReconciler) getMachineState(machineID string) (api.MachineState, error) {
	domainState, _, err := r.host.Libvirt().DomainGetState(machineDomain(machineID), 0)
	if err != nil {
		return "", fmt.Errorf("error getting domain state: %w", err)
	}

	if machineState, ok := domainStateToMachineState[libvirt.DomainState(domainState)]; ok {
		return machineState, nil
	}
	return api.MachineStatePending, nil
}

func (r *MachineReconciler) createDomain(
	ctx context.Context,
	log logr.Logger,
	machine *api.Machine,
) ([]api.VolumeStatus, []api.NetworkInterfaceStatus, error) {
	generatedDomainXML, volumeStates, nicStates, err := r.domainFor(ctx, log, machine)
	if err != nil {
		return volumeStates, nicStates, err
	}

	domainXML := providerlibvirtxml.MergeDomains(r.overrideDomainXML, generatedDomainXML)
	domainXMLData, err := domainXML.Marshal()
	if err != nil {
		return volumeStates, nicStates, err
	}

	log.V(1).Info("Creating domain")
	log.V(2).Info("Domain", "XML", domainXMLData)
	if _, err := r.host.Libvirt().DomainCreateXML(domainXMLData, libvirt.DomainNone); err != nil {
		return volumeStates, nicStates, err
	}

	return volumeStates, nicStates, nil
}

func detectArchitecture() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return ArchitectureX8664, nil
	case "arm64":
		return ArchitectureAARCH64, nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}
}

func (r *MachineReconciler) domainFor(
	ctx context.Context,
	log logr.Logger,
	machine *api.Machine,
) (*libvirtxml.Domain, []api.VolumeStatus, []api.NetworkInterfaceStatus, error) {
	architecture, err := detectArchitecture()
	if err != nil {
		return nil, nil, nil, err
	}

	osType := guest.OSTypeHVM // TODO: Make this configurable via machine class
	domainSettings, err := r.guestCapabilities.SettingsFor(guest.Requests{
		Architecture: architecture,
		OSType:       osType,
	})

	if err != nil {
		return nil, nil, nil, err
	}

	cpu := &libvirtxml.DomainCPU{}
	if architecture != ArchitectureAARCH64 && domainSettings.Type != "qemu" {
		cpu.Mode = "host-passthrough"
	}

	var serialTargetType string
	switch architecture {
	case ArchitectureAARCH64:
		if domainSettings.Type == "qemu" {
			cpu.Model = &libvirtxml.DomainCPUModel{
				Value: "max",
			}
		}
		serialTargetType = "system-serial"
	case ArchitectureX8664:
		serialTargetType = "pci-serial"
	}

	domainDesc := &libvirtxml.Domain{
		Name:       machine.GetID(),
		UUID:       machine.GetID(),
		Type:       domainSettings.Type,
		OnPoweroff: "destroy",
		OnReboot:   "restart",
		OnCrash:    "coredump-restart",
		CPU:        cpu,
		Features: &libvirtxml.DomainFeatureList{
			ACPI: &libvirtxml.DomainFeature{},
			APIC: &libvirtxml.DomainFeatureAPIC{},
		},
		OS: &libvirtxml.DomainOS{
			Type: &libvirtxml.DomainOSType{
				Type:    string(osType),
				Arch:    architecture,
				Machine: domainSettings.Machine,
			},
			BootDevices: []libvirtxml.DomainBootDevice{
				{Dev: "hd"},
			},
			Firmware: "efi",
			FirmwareInfo: &libvirtxml.DomainOSFirmwareInfo{
				Features: []libvirtxml.DomainOSFirmwareFeature{
					{
						Name:    "secure-boot",
						Enabled: "no",
					},
				},
			},
		},
		Clock: &libvirtxml.DomainClock{
			Offset: "utc",
			Timer: []libvirtxml.DomainTimer{
				{
					Name:       "rtc",
					TickPolicy: "catchup",
				},
				{
					Name:       "hpet",
					TickPolicy: "catchup",
				},
				{
					Name:       "tsc",
					Mode:       "paravirt",
					TickPolicy: "catchup",
				},
			},
		},
		Devices: &libvirtxml.DomainDeviceList{
			Serials: []libvirtxml.DomainSerial{
				{
					Target: &libvirtxml.DomainSerialTarget{
						Type: serialTargetType,
					},
				},
			},
			Consoles: []libvirtxml.DomainConsole{
				{
					TTY: "pty",
					Target: &libvirtxml.DomainConsoleTarget{
						Type: "serial",
					},
				},
			},
			//Watchdog: &libvirtxml.DomainWatchdog{  // TODO: Add Watchdog again with proper libvirt version
			//	Model:  "i6300esb",
			//	Action: "reset",
			//},
			RNGs: []libvirtxml.DomainRNG{
				{
					Model: "virtio",
					Rate: &libvirtxml.DomainRNGRate{
						Bytes: 512,
					},
					Backend: &libvirtxml.DomainRNGBackend{
						Random: &libvirtxml.DomainRNGBackendRandom{},
					},
				},
			},
		},
	}

	if err := r.setDomainMetadata(log, machine, domainDesc); err != nil {
		return nil, nil, nil, err
	}

	if err := r.setDomainResources(machine, domainDesc); err != nil {
		return nil, nil, nil, err
	}

	if err := r.setDomainPCIControllers(domainDesc, machine.Spec.PCIControllerTotal); err != nil {
		return nil, nil, nil, err
	}

	sgx.EnableSGXInDomain(&machine.Spec, domainDesc)

	if machine.Spec.GuestAgent != api.GuestAgentNone {
		r.setGuestAgent(machine, domainDesc)
	}

	if machineImgRef := machine.Spec.Image; machineImgRef != nil && ptr.Deref(machineImgRef, "") != "" {
		if err := r.setDomainImage(ctx, log, machine, domainDesc, ptr.Deref(machineImgRef, "")); err != nil {
			return nil, nil, nil, err
		}
	}

	if ignitionSpec := machine.Spec.Ignition; ignitionSpec != nil {
		if err := r.setDomainIgnition(machine, domainDesc); err != nil {
			return nil, nil, nil, err
		}
	} else {
		r.Eventf(log, machine.Metadata, corev1.EventTypeWarning, "NoIgnitionData", "Machine does not have ignition data")
	}

	attacher, err := NewLibvirtVolumeAttacher(domainDesc, NewCreateDomainExecutor(r.host.Libvirt()), r.volumeCachePolicyCeph)
	if err != nil {
		return nil, nil, nil, err
	}

	volumeStates, err := r.reconcileVolumes(ctx, log, machine, attacher)
	if err != nil {
		r.Eventf(log, machine.Metadata, corev1.EventTypeWarning, "reconcileVolumes", "Volume reconciliation failed with error: %s", err)
		return nil, volumeStates, nil, err
	}
	if machine.Spec.Volumes != nil {
		r.Eventf(log, machine.Metadata, corev1.EventTypeNormal, "AttchedVolume", "Successfully attached volumes")
	}

	nicStatesAsPointers, err := r.setDomainNetworkInterfaces(ctx, machine, domainDesc)
	nicStates := removePointerFromNicsStatusArray(nicStatesAsPointers)
	if err != nil {
		r.Eventf(log, machine.Metadata, corev1.EventTypeWarning, "setDomainNetworkInterfaces", "Setting domain network interface failed with error: %s", err)
		return nil, volumeStates, nicStates, err
	}
	if machine.Spec.NetworkInterfaces != nil {
		r.Eventf(log, machine.Metadata, corev1.EventTypeNormal, "AttchedNIC", "Successfully attached network interfaces")
	}

	if err := r.setPCIDevices(machine, domainDesc); err != nil {
		return nil, volumeStates, nicStates, err
	}

	return domainDesc, volumeStates, nicStates, nil
}

func (r *MachineReconciler) setDomainMetadata(log logr.Logger, machine *api.Machine, domain *libvirtxml.Domain) error {
	labels, found := machine.Annotations[api.LabelsAnnotation]
	if !found {
		log.V(1).Info("IRI machine labels are not annotated in the API machine")
		return nil
	}
	var irimachineLabels map[string]string
	err := json.Unmarshal([]byte(labels), &irimachineLabels)
	if err != nil {
		return fmt.Errorf("error unmarshalling iri machine labels: %w", err)
	}

	encodedLabels := libvirtmeta.IRIMachineLabelsEncoder(irimachineLabels)

	domainMetadata := &libvirtmeta.LibvirtProviderMetadata{
		IRIMmachineLabels: encodedLabels,
	}

	domainMetadataXML, err := xml.Marshal(domainMetadata)
	if err != nil {
		return err
	}
	domain.Metadata = &libvirtxml.DomainMetadata{
		XML: string(domainMetadataXML),
	}
	return nil
}

func (r *MachineReconciler) setDomainResources(machine *api.Machine, domain *libvirtxml.Domain) error {
	memory := machine.Spec.Resources[core.ResourceMemory]
	domain.Memory = &libvirtxml.DomainMemory{
		Value: uint(memory.Value()),
		Unit:  "Byte",
	}

	hugepages, ok := machine.Spec.Resources[sources.ResourceHugepages]
	if ok && hugepages.Value() > 0 {
		domain.MemoryBacking = &libvirtxml.DomainMemoryBacking{
			MemoryHugePages: &libvirtxml.DomainMemoryHugepages{},
		}
	}

	cpu := machine.Spec.Resources[core.ResourceCPU]
	domain.VCPU = &libvirtxml.DomainVCPU{
		Value: uint(cpu.Value()),
	}

	return nil
}

// TODO: Investigate hotplugging the pcie-root-port controllers with disks.
// Ref: https://libvirt.org/pci-hotplug.html#x86_64-q35
func (r *MachineReconciler) setDomainPCIControllers(domain *libvirtxml.Domain, pciControllerTotal int) error {
	domain.Devices.Controllers = append(domain.Devices.Controllers, libvirtxml.DomainController{
		Type:  "pci",
		Model: "pcie-root",
	})

	// Adding 3 more PCIe root ports because the root disk, memballoon, and rng each consume one PCIe slot by default.
	const defaultDomainPCICount = 3

	for i := 1; i <= pciControllerTotal+defaultDomainPCICount; i++ {
		domain.Devices.Controllers = append(domain.Devices.Controllers, libvirtxml.DomainController{
			Type:  "pci",
			Model: "pcie-root-port",
		})
	}
	return nil
}

func (r *MachineReconciler) setGuestAgent(machine *api.Machine, domainDesc *libvirtxml.Domain) {
	if domainDesc.Devices == nil {
		domainDesc.Devices = &libvirtxml.DomainDeviceList{}
	}

	if domainDesc.Devices.Channels == nil {
		domainDesc.Devices.Channels = make([]libvirtxml.DomainChannel, 0, 1)
	}

	socketPath := filepath.Join(r.host.MachineDir(machine.GetID()), "qemu-guest-agent.sock")
	agent := libvirtxml.DomainChannel{
		Source: &libvirtxml.DomainChardevSource{
			UNIX: &libvirtxml.DomainChardevSourceUNIX{
				Mode: "bind",
				Path: socketPath,
			},
		},
		Target: &libvirtxml.DomainChannelTarget{
			VirtIO: &libvirtxml.DomainChannelTargetVirtIO{
				Name: "org.qemu.guest_agent.0",
			},
		},
	}

	domainDesc.Devices.Channels = append(domainDesc.Devices.Channels, agent)
	machine.Status.GuestAgentStatus = &api.GuestAgentStatus{Addr: "unix://" + socketPath}
}

func (r *MachineReconciler) setPCIDevices(machine *api.Machine, domain *libvirtxml.Domain) error {
	devices := machine.Status.PCIDevices
	for index := range devices {
		domain.Devices.Hostdevs = append(domain.Devices.Hostdevs, libvirtxml.DomainHostdev{
			Managed:   "yes",
			SubsysPCI: devices[index].Addr.GetDomainSubsysPCI(),
		})
	}
	return nil
}

func (r *MachineReconciler) setDomainImage(
	ctx context.Context,
	log logr.Logger,
	machine *api.Machine,
	domain *libvirtxml.Domain,
	machineImgRef string,
) error {
	img, err := r.imageCache.Get(ctx, machineImgRef)
	if err != nil {
		if !errors.Is(err, providerimage.ErrImagePulling) {
			return err
		}
		r.Eventf(log, machine.Metadata, corev1.EventTypeNormal, "PullingImage", "Pulling image %s", machineImgRef)
		return err
	}

	rootFSFile := r.host.MachineRootFSFile(machine.ID)
	ok, err := osutils.RegularFileExists(rootFSFile)
	if err != nil {
		return err
	}
	if !ok {
		if err := r.raw.Create(rootFSFile, raw.WithSourceFile(img.SquashFS.Path)); err != nil {
			return fmt.Errorf("error creating root fs disk: %w", err)
		}
		if err := os.Chmod(rootFSFile, permFile); err != nil {
			return fmt.Errorf("error changing root fs disk mode: %w", err)
		}
	}

	domain.OS.Kernel = img.Kernel.Path
	domain.OS.Initrd = img.InitRAMFs.Path
	domain.OS.Cmdline = img.Config.CommandLine
	domain.Devices.Disks = append(domain.Devices.Disks, libvirtxml.DomainDisk{
		Alias: &libvirtxml.DomainAlias{
			Name: rootFSAlias,
		},
		Device: "disk",
		Driver: &libvirtxml.DomainDiskDriver{
			Name:  "qemu",
			Type:  "raw",
			Cache: "none",
		},
		Source: &libvirtxml.DomainDiskSource{
			File: &libvirtxml.DomainDiskSourceFile{
				File: rootFSFile,
			},
		},
		Target: &libvirtxml.DomainDiskTarget{
			Dev: "vdaaa", // TODO: Reserving vdaaa for ramdisk, so that it doesnt conflict with other volumes, investigate better solution.
			Bus: "virtio",
		},
		Serial:   "machineboot",
		ReadOnly: &libvirtxml.DomainDiskReadOnly{},
	})
	return nil
}

func (r *MachineReconciler) setDomainIgnition(machine *api.Machine, domain *libvirtxml.Domain) error {
	ignitionData := machine.Spec.Ignition

	ignPath := r.host.MachineIgnitionFile(machine.ID)
	if err := os.WriteFile(ignPath, ignitionData, permFileIgnition); err != nil {
		return err
	}

	domain.SysInfo = append(domain.SysInfo, libvirtxml.DomainSysInfo{
		FWCfg: &libvirtxml.DomainSysInfoFWCfg{
			Entry: []libvirtxml.DomainSysInfoEntry{
				{
					// TODO: Make the ignition sysinfo key configurable via ironcore-image / machine spec.
					Name: libvirtDomainXMLIgnitionKeyName,
					File: ignPath,
				},
			},
		},
	})
	return nil
}

func (r *MachineReconciler) getDomainDesc(machineID string) (*libvirtxml.Domain, error) {
	domainXMLData, err := r.host.Libvirt().DomainGetXMLDesc(libvirt.Domain{UUID: libvirtutils.UUIDStringToBytes(machineID)}, 0)
	if err != nil {
		return nil, err
	}

	domainXML := &libvirtxml.Domain{}
	if err := domainXML.Unmarshal(domainXMLData); err != nil {
		return nil, err
	}
	return domainXML, nil
}

func (r *MachineReconciler) updateAPIMachineStatus(ctx context.Context, machine *api.Machine, state api.MachineState, volumes []api.VolumeStatus, nics []api.NetworkInterfaceStatus) error {
	// TODO: we can rewrite reconcile function for return whole new status structure.
	requireUpdate := false

	if state != "" && machine.Status.State != state {
		requireUpdate = true
		machine.Status.State = state
	}

	if volumes != nil {
		requireUpdate = true
		machine.Status.VolumeStatus = volumes
	}

	if nics != nil {
		requireUpdate = true
		machine.Status.NetworkInterfaceStatus = nics
	}

	if requireUpdate {
		_, err := r.machines.Update(ctx, machine)
		if err != nil {
			return err
		}
	}

	return nil
}

func machineDomain(machineID string) libvirt.Domain {
	return libvirt.Domain{
		UUID: libvirtutils.UUIDStringToBytes(machineID),
	}
}

func removePointerFromNicsStatusArray(nics []*api.NetworkInterfaceStatus) []api.NetworkInterfaceStatus {
	result := make([]api.NetworkInterfaceStatus, 0, len(nics))
	for index := range nics {
		result = append(result, *(nics[index]))
	}
	return result
}
