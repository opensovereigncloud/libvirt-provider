// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"errors"
	goflag "flag"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	grpcprometheus "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"github.com/ironcore-dev/ironcore-image/oci/remote"
	ocistore "github.com/ironcore-dev/ironcore-image/oci/store"
	apinetv1alpha1 "github.com/ironcore-dev/ironcore-net/api/core/v1alpha1"
	"github.com/ironcore-dev/ironcore/broker/common"
	commongrpc "github.com/ironcore-dev/ironcore/broker/common/grpc"
	iri "github.com/ironcore-dev/ironcore/iri/apis/machine/v1alpha1"
	"github.com/ironcore-dev/libvirt-provider/api"

	"github.com/ironcore-dev/libvirt-provider/internal/apinetwatcher"
	"github.com/ironcore-dev/libvirt-provider/internal/console"
	"github.com/ironcore-dev/libvirt-provider/internal/controllers"
	"github.com/ironcore-dev/libvirt-provider/internal/event"
	"github.com/ironcore-dev/libvirt-provider/internal/event/machineevent"
	"github.com/ironcore-dev/libvirt-provider/internal/healthcheck"
	"github.com/ironcore-dev/libvirt-provider/internal/host"
	"github.com/ironcore-dev/libvirt-provider/internal/libvirt/guest"
	libvirtutils "github.com/ironcore-dev/libvirt-provider/internal/libvirt/utils"
	providerlibvirtxml "github.com/ironcore-dev/libvirt-provider/internal/libvirtxml"
	"github.com/ironcore-dev/libvirt-provider/internal/metrics"
	"github.com/ironcore-dev/libvirt-provider/internal/networkinterfaceplugin"
	"github.com/ironcore-dev/libvirt-provider/internal/oci"
	ociutils "github.com/ironcore-dev/libvirt-provider/internal/oci/utils"
	volumeplugin "github.com/ironcore-dev/libvirt-provider/internal/plugins/volume"
	"github.com/ironcore-dev/libvirt-provider/internal/plugins/volume/ceph"
	"github.com/ironcore-dev/libvirt-provider/internal/plugins/volume/emptydisk"
	"github.com/ironcore-dev/libvirt-provider/internal/raw"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/manager"
	"github.com/ironcore-dev/libvirt-provider/internal/resources/sources"
	"github.com/ironcore-dev/libvirt-provider/internal/server"
	"github.com/ironcore-dev/libvirt-provider/internal/strategy"
	internalutils "github.com/ironcore-dev/libvirt-provider/internal/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	homeDir string
)

const (
	HTTPServerReadTimeout     = 200 * time.Millisecond
	HTTPServerWriteTimeout    = 200 * time.Millisecond
	HTTPServerIdleTimeout     = 1 * time.Second
	HTTPServerGracefulTimeout = 2 * time.Second
)

func init() {
	homeDir, _ = os.UserHomeDir()
}

type Options struct {
	BaseURL string

	Servers ServersOptions

	RootDir string

	PathSupportedMachineClasses string

	GuestAgent GuestAgentOption

	Libvirt   LibvirtOptions
	NicPlugin *networkinterfaceplugin.Options

	GCVMGracefulShutdownTimeout        time.Duration
	ResyncIntervalGarbageCollector     time.Duration
	EventListWatchSourceResyncDuration time.Duration

	ResourceManagerOptions sources.Options

	MachineEventStore machineevent.EventStoreOptions

	VolumeCachePolicyCeph string
}

type HTTPServerOptions struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	GracefulTimeout time.Duration
}

type GRPCServerOptions struct {
	Addr              string
	ConnectionTimeout time.Duration
}

type ServersOptions struct {
	Metrics     HTTPServerOptions
	HealthCheck HTTPServerOptions
	PPROF       HTTPServerOptions
	Streaming   HTTPServerOptions
	GRPC        GRPCServerOptions
}

type LibvirtOptions struct {
	Socket  string
	Address string
	URI     string

	PreferredDomainTypes  []string
	PreferredMachineTypes []string

	OverrideDomainXML string

	PCIControllerTotal int
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	// ServerOptions
	fs.StringVar(&o.Servers.GRPC.Addr, "servers-grpc-address", "/var/run/iri-machinebroker.sock", "Address to listen on.")
	fs.DurationVar(&o.Servers.GRPC.ConnectionTimeout, "servers-grpc-connectiontimeout", 3*time.Second, "Connection timeout for GRPC server.")

	fs.StringVar(&o.Servers.Streaming.Addr, "servers-streaming-address", "127.0.0.1:20251", "Address at which the stream server will listen")
	fs.DurationVar(&o.Servers.Streaming.ReadTimeout, "servers-streaming-readtimeout", HTTPServerReadTimeout, "Read timeout for streaming server.")
	fs.DurationVar(&o.Servers.Streaming.WriteTimeout, "servers-streaming-writetimeout", HTTPServerWriteTimeout, "Write timeout for streaming server.")
	fs.DurationVar(&o.Servers.Streaming.IdleTimeout, "servers-streaming-idletimeout", HTTPServerIdleTimeout, "Idle timeout for connections to streaming server.")
	fs.DurationVar(&o.Servers.Streaming.GracefulTimeout, "servers-streaming-gracefultimeout", HTTPServerGracefulTimeout, "Graceful timeout to shutdown streaming server. Ideally set it little longer than idletimeout.")

	fs.StringVar(&o.Servers.Metrics.Addr, "servers-metrics-address", "", "Address to listen on exposing of metrics. If address isn't set, server is disabled.")
	fs.DurationVar(&o.Servers.Metrics.ReadTimeout, "servers-metrics-readtimeout", HTTPServerReadTimeout, "Read timeout for metrics server.")
	fs.DurationVar(&o.Servers.Metrics.WriteTimeout, "servers-metrics-writetimeout", HTTPServerWriteTimeout, "Write timeout for metrics server.")
	fs.DurationVar(&o.Servers.Metrics.IdleTimeout, "servers-metrics-idletimeout", HTTPServerIdleTimeout, "Idle timeout for connections to metrics server.")
	fs.DurationVar(&o.Servers.Metrics.GracefulTimeout, "servers-metrics-gracefultimeout", HTTPServerGracefulTimeout, "Graceful timeout for shutdown metrics server.")

	fs.StringVar(&o.Servers.HealthCheck.Addr, "servers-health-check-address", ":8181", "Address to listen on health check liveness.")
	fs.DurationVar(&o.Servers.HealthCheck.ReadTimeout, "servers-health-check-readtimeout", HTTPServerReadTimeout, "Read timeout for health check server.")
	fs.DurationVar(&o.Servers.HealthCheck.WriteTimeout, "servers-health-check-writetimeout", HTTPServerWriteTimeout, "Write timeout for health check server.")
	fs.DurationVar(&o.Servers.HealthCheck.IdleTimeout, "servers-health-check-idletimeout", HTTPServerIdleTimeout, "Idle timeout for connections to health check server.")
	fs.DurationVar(&o.Servers.HealthCheck.GracefulTimeout, "servers-health-check-gracefultimeout", HTTPServerGracefulTimeout, "Graceful timeout for shutdown health check server.")

	fs.StringVar(&o.Servers.PPROF.Addr, "servers-pprof-address", "", "Address to listen on exposing of pprof. If address isn't set, server is disabled.")
	fs.DurationVar(&o.Servers.PPROF.ReadTimeout, "servers-pprof-readtimeout", HTTPServerReadTimeout, "Read timeout for pprof server.")
	fs.DurationVar(&o.Servers.PPROF.WriteTimeout, "servers-pprof-writetimeout", HTTPServerWriteTimeout, "Write timeout for pprof server.")
	fs.DurationVar(&o.Servers.PPROF.IdleTimeout, "servers-pprof-idletimeout", HTTPServerIdleTimeout, "Idle timeout for connections to pprof server.")
	fs.DurationVar(&o.Servers.PPROF.GracefulTimeout, "servers-pprof-gracefultimeout", HTTPServerGracefulTimeout, "Graceful timeout for shutdown pprof server.")

	fs.StringVar(&o.RootDir, "libvirt-provider-dir", filepath.Join(homeDir, ".libvirt-provider"), "Path to the directory libvirt-provider manages its content at.")

	fs.StringVar(&o.PathSupportedMachineClasses, "supported-machine-classes", o.PathSupportedMachineClasses, "File containing supported machine classes.")

	fs.StringVar(&o.BaseURL, "base-url", "", "The base url to construct urls for streaming from. If empty it will be "+
		"constructed from the streaming-address")

	fs.Var(&o.GuestAgent, "guest-agent-type", fmt.Sprintf("Guest agent implementation to use. Available: %v", guestAgentOptionAvailable()))

	// LibvirtOptions
	fs.StringVar(&o.Libvirt.Socket, "libvirt-socket", o.Libvirt.Socket, "Path to the libvirt socket to use.")
	fs.StringVar(&o.Libvirt.Address, "libvirt-address", o.Libvirt.Address, "Address of a RPC libvirt socket to connect to.")
	fs.StringVar(&o.Libvirt.URI, "libvirt-uri", o.Libvirt.URI, "URI to connect to inside the libvirt system.")
	fs.StringVar(&o.Libvirt.OverrideDomainXML, "libvirt-override-template", o.Libvirt.OverrideDomainXML, "Path to the override domain XML template used for VM creation.")
	fs.IntVar(&o.Libvirt.PCIControllerTotal, "libvirt-domain-pci-total", 30, "Total number of PCI controllers to be configured in the domain XML.")

	// Guest Capabilities
	fs.StringSliceVar(&o.Libvirt.PreferredDomainTypes, "preferred-domain-types", []string{"kvm", "qemu"}, "Ordered list of preferred domain types to use.")
	fs.StringSliceVar(&o.Libvirt.PreferredMachineTypes, "preferred-machine-types", []string{"pc-q35"}, "Ordered list of preferred machine types to use.")

	fs.DurationVar(&o.GCVMGracefulShutdownTimeout, "gc-vm-graceful-shutdown-timeout", 5*time.Minute, "Duration to wait for the VM to gracefully shut down. If the VM does not shut down within this period, it will be forcibly destroyed by garbage collector.")
	fs.DurationVar(&o.ResyncIntervalGarbageCollector, "gc-resync-interval", 1*time.Minute, "Interval for resynchronizing the garbage collector.")
	fs.DurationVar(&o.EventListWatchSourceResyncDuration, "event-list-watch-source-resync-duration", 1*time.Hour, "Duration for resynchronizing the list and watch events of source. Set 0 or negative to disable resync.")

	fs.StringSliceVar(&o.ResourceManagerOptions.Sources, "resource-manager-sources", []string{"cpu", "memory"}, fmt.Sprintf("Sources for loading resources. Available: %v", manager.GetSourcesAvailable()))
	fs.Float64Var(&o.ResourceManagerOptions.OvercommitVCPU, "resource-manager-overcommit-vcpu", 1.0, "Sets the overcommit ratio for vCPUs, enabling higher VM density per CPU core.")
	fs.Uint64Var(&o.ResourceManagerOptions.BlockedHugepages, "resource-manager-blocked-hugepages", 0, "Count of hugepages which aren't use for VMs. Effective only if hugepages source is set")
	fs.Var(&o.ResourceManagerOptions.ReservedMemorySize, "resource-manager-reserved-memory-size", "Size of memory which aren't use for VMs in human-readable format. Effective only if memory source is set")
	fs.Uint64Var(&o.ResourceManagerOptions.VMLimit, "resource-manager-vm-limit", 0, "Maximum number of the VMs to be created on the host")
	fs.StringVar(&o.ResourceManagerOptions.PCIDevicesFile, "resource-manager-pci-devices-file", "", "yaml file with list of supported pci devices for pci source.")

	// Machine event store options
	fs.IntVar(&o.MachineEventStore.MaxEvents, "machine-event-max-events", 100, "Maximum number of machine events that can be stored.")
	fs.DurationVar(&o.MachineEventStore.TTL, "machine-event-ttl", 5*time.Minute, "Time to live for machine events.")
	fs.DurationVar(&o.MachineEventStore.ResyncInterval, "machine-event-resync-interval", 1*time.Minute, "Interval for resynchronizing the machine events.")

	// Volume cache policy option
	fs.StringVar(&o.VolumeCachePolicyCeph, "volume-cache-policy-ceph", "none",
		`Policy to use when creating a remote disk. (one of 'none', 'writeback', 'writethrough', 'directsync', 'unsafe').
Note: The available options may depend on the hypervisor and libvirt version in use.
Please refer to the official documentation for more details: https://libvirt.org/formatdomain.html#hard-drives-floppy-disks-cdroms.`)

	o.NicPlugin = networkinterfaceplugin.NewDefaultOptions()
	o.NicPlugin.AddFlags(fs)
}

func (o *Options) MarkFlagsRequired(cmd *cobra.Command) {
	_ = cmd.MarkFlagRequired("supported-machine-classes")
}

func Command() *cobra.Command {
	var (
		zapOpts = zap.Options{Development: true}
		opts    Options
	)

	cmd := &cobra.Command{
		Use: "libvirt-provider",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logger := zap.New(zap.UseFlagOptions(&zapOpts))
			ctrl.SetLogger(logger)
			cmd.SetContext(ctrl.LoggerInto(cmd.Context(), ctrl.Log))
		},
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			defer func() {
				if r := recover(); r != nil {
					internalutils.LogPanic(ctrl.Log, r, "RunE")
					err = errors.Join(err, fmt.Errorf("%v", r))
				}
			}()
			// flag parsing is done therefore we can silence the usage message
			cmd.SilenceUsage = true
			// error logging is done in the main
			cmd.SilenceErrors = true

			err = Run(cmd.Context(), opts)

			return
		},
	}

	goFlags := goflag.NewFlagSet("", 0)
	zapOpts.BindFlags(goFlags)
	cmd.PersistentFlags().AddGoFlagSet(goFlags)

	opts.AddFlags(cmd.Flags())
	opts.MarkFlagsRequired(cmd)

	return cmd
}

func Run(ctx context.Context, opts Options) error {
	log := ctrl.LoggerFrom(ctx)
	setupLog := log.WithName("setup")

	// Setup Libvirt Client
	libvirt, err := libvirtutils.GetLibvirt(opts.Libvirt.Socket, opts.Libvirt.Address, opts.Libvirt.URI)
	if err != nil {
		setupLog.Error(err, "failed to initialize libvirt")
		return err
	}
	defer func() {
		if err := libvirt.ConnectClose(); err != nil {
			setupLog.Error(err, "failed to close libvirt connection")
		}
	}()

	baseURL := opts.BaseURL
	if baseURL == "" {
		u := &url.URL{
			Scheme: "http",
			Host:   opts.Servers.Streaming.Addr,
		}
		baseURL = u.String()
	}

	providerHost, err := host.NewLibvirtAt(opts.RootDir, libvirt)
	if err != nil {
		setupLog.Error(err, "failed to initialize provider host")
		return err
	}

	platform, err := ociutils.Platform()
	if err != nil {
		setupLog.Error(err, "failed to get host platform: %w", err)
		return err
	}
	setupLog.Info("Current platform", "architecture", platform.Architecture)

	reg, err := remote.DockerRegistryWithPlatform(nil, platform)
	if err != nil {
		setupLog.Error(err, "failed to initialize registry")
		return err
	}

	ociStore, err := ocistore.New(providerHost.ImagesDir())
	if err != nil {
		setupLog.Error(err, "error creating oci store")
		return err
	}

	imgCache, err := oci.NewLocalCache(log.WithName("oci-local-cache"), reg, ociStore, nil)
	if err != nil {
		setupLog.Error(err, "failed to initialize oci manager")
		return err
	}

	rawInst, err := raw.Instance(raw.Default())
	if err != nil {
		setupLog.Error(err, "failed to initialize raw instance")
		return err
	}

	// Detect Guest Capabilities
	caps, err := guest.DetectCapabilities(libvirt, guest.CapabilitiesOptions{
		PreferredDomainTypes:  opts.Libvirt.PreferredDomainTypes,
		PreferredMachineTypes: opts.Libvirt.PreferredMachineTypes,
	})
	if err != nil {
		setupLog.Error(err, "failed to detect guest capabilities")
		return err
	}

	volumePlugins := volumeplugin.NewPluginManager()
	if err := volumePlugins.InitPlugins(providerHost, []volumeplugin.Plugin{
		ceph.NewPlugin(),
		emptydisk.NewPlugin(rawInst),
	}); err != nil {
		setupLog.Error(err, "failed to initialize volume plugin manager")
		return err
	}

	nicPlugin, watcher, err := opts.NicPlugin.NetworkInterfacePlugin()
	if err != nil {
		setupLog.Error(err, "failed to initialize network plugin")
		return err
	}

	setupLog.Info("Configuring machine store", "Directory", providerHost.MachineStoreDir())
	machineStore, err := host.NewStore(host.Options[*api.Machine]{
		NewFunc:        func() *api.Machine { return &api.Machine{} },
		CreateStrategy: strategy.MachineStrategy,
		Dir:            providerHost.MachineStoreDir(),
		Logger:         log,
	})
	if err != nil {
		setupLog.Error(err, "failed to initialize machine store")
		return err
	}

	errs := machineStore.CleanupSwapFiles()
	// these errors don't affect bussines logic
	if len(errs) > 0 {
		for _, err := range errs {
			setupLog.Error(err, "failed to remove all swap files from machine store")
		}

		return fmt.Errorf("failed to cleanup machine store")
	}

	err = initMetrics(ctx, log, machineStore.List)
	if err != nil {
		setupLog.Error(err, "failed to initialize metrics")
		return err
	}

	opts.ResourceManagerOptions.Log = log
	err = initResourceManager(ctx, opts.ResourceManagerOptions, machineStore, opts.PathSupportedMachineClasses)
	if err != nil {
		setupLog.Error(err, "failed to initialize resource manager")
		return err
	}

	machineEvents, err := event.NewListWatchSource[*api.Machine](
		machineStore.List,
		machineStore.Watch,
		event.ListWatchSourceOptions{
			ResyncDuration: opts.EventListWatchSourceResyncDuration,
			Logger:         log.WithName("machine-events"),
		},
	)
	if err != nil {
		setupLog.Error(err, "failed to initialize machine events")
		return err
	}

	eventStore := machineevent.NewEventStore(log.WithName("iri-event-store"), opts.MachineEventStore)

	overrideDomainXML, err := providerlibvirtxml.LoadOverrideDomainXML(opts.Libvirt.OverrideDomainXML)
	if err != nil {
		setupLog.Error(err, "failed to load override domain template XML")
		return err
	}

	controllerLogger := log.WithName(controllers.MachineReconcilerName)

	queue := workqueue.NewTypedRateLimitingQueueWithConfig[string](workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{
			Name:            controllers.MachineReconcilerName,
			MetricsProvider: metrics.NewWorkqueueMetricsProvider(controllerLogger.WithName(controllers.MachineReconcilerMetrics)),
		},
	)

	var watcherEmitter apinetwatcher.EventEmitter[*apinetv1alpha1.NetworkInterface]
	if watcher != nil {
		watcherEmitter = apinetwatcher.NewEventEmitter[*apinetv1alpha1.NetworkInterface]()
	}

	machineReconciler, err := controllers.NewMachineReconciler(
		controllerLogger,
		providerHost,
		machineStore,
		machineEvents,
		eventStore,
		controllers.MachineReconcilerOptions{
			GuestCapabilities:              caps,
			ImageCache:                     imgCache,
			Raw:                            rawInst,
			VolumePluginManager:            volumePlugins,
			NetworkInterfacePlugin:         nicPlugin,
			ResyncIntervalGarbageCollector: opts.ResyncIntervalGarbageCollector,
			GCVMGracefulShutdownTimeout:    opts.GCVMGracefulShutdownTimeout,
			VolumeCachePolicyCeph:          opts.VolumeCachePolicyCeph,
			OverrideDomainXML:              overrideDomainXML,
			Queue:                          queue,
			WatcherEmitter:                 watcherEmitter,
		},
	)
	if err != nil {
		setupLog.Error(err, "failed to initialize machine controller")
		return err
	}

	srv, err := server.New(server.Options{
		BaseURL:            baseURL,
		Libvirt:            libvirt,
		MachineStore:       machineStore,
		EventStore:         eventStore,
		VolumePlugins:      volumePlugins,
		NetworkPlugins:     nicPlugin,
		PCIControllerTotal: opts.Libvirt.PCIControllerTotal,
		GuestAgent:         opts.GuestAgent.GetAPIGuestAgent(),
	})
	if err != nil {
		setupLog.Error(err, "failed to initialize server")
		return err
	}

	healthCheck := healthcheck.HealthCheck{
		Libvirt: libvirt,
		Log:     log.WithName("health-check"),
	}

	g, ctx := errgroup.WithContext(ctx)

	// Start watcher only if it's initialized (i.e. we're using apinet)
	if watcher != nil {
		g.Go(func() error {
			setupLog.Info("Starting APINet watcher")
			locErr := watcher.Start(ctx, watcherEmitter)
			if locErr != nil {
				return fmt.Errorf("error running APINet watcher: %w", locErr)
			}
			return nil
		})

		// Ensure informer caches are fully synced before processing events.
		// Prevents acting on stale/empty data during startup.
		err = watcher.WaitForCacheSync(ctx)
		if err != nil {
			return fmt.Errorf("failed to synchronize apinet client cache: %w", err)
		}
	}

	setupLog.Info("Initializing network interface plugin")

	if err := nicPlugin.Init(ctx, providerHost); err != nil {
		setupLog.Error(err, "failed to initialize network plugin")
		return err
	}

	g.Go(func() error {
		return runMetricsServer(ctx, setupLog, opts.Servers.Metrics)
	})

	g.Go(func() error {
		setupLog.Info("Starting oci cache")
		if err := imgCache.Start(ctx); err != nil {
			setupLog.Error(err, "failed to start oci cache")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting machine reconciler")
		if err := machineReconciler.Start(ctx); err != nil {
			setupLog.Error(err, "failed to start machine reconciler")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting machine events")
		if err := machineEvents.Start(ctx); err != nil {
			setupLog.Error(err, "failed to start machine events")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting machine events garbage collector")
		eventStore.Start(ctx)
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting grpc server")
		if err := runGRPCServer(ctx, setupLog, log, srv, opts.Servers.GRPC); err != nil {
			setupLog.Error(err, "failed to start grpc server")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting streaming server")
		if err := runStreamingServer(ctx, setupLog, log, srv, opts.Servers.Streaming); err != nil {
			setupLog.Error(err, "failed to start streaming server")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting health check server")
		if err := runHealthCheckServer(ctx, setupLog, log, healthCheck, opts.Servers.HealthCheck); err != nil {
			setupLog.Error(err, "failed to start health check server")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting pprof server")
		if err := runPPROFServer(ctx, setupLog, log, opts.Servers.PPROF); err != nil {
			setupLog.Error(err, "failed to start pprof server")
			return err
		}
		return nil
	})

	g.Go(func() error {
		setupLog.Info("Starting handling libvirt events")
		if err := libvirtutils.HandleEvents(ctx, log.WithName("libvirt-event"), libvirt, machineStore, queue); err != nil {
			setupLog.Error(err, "failed to run libvirt events handling")
		}
		return nil
	})

	return g.Wait()
}

func runGRPCServer(ctx context.Context, setupLog, log logr.Logger, srv *server.Server, opts GRPCServerOptions) error {
	setupLog.V(1).Info("Cleaning up any previous socket")
	if err := common.CleanupSocketIfExists(opts.Addr); err != nil {
		return fmt.Errorf("error cleaning up socket: %w", err)
	}

	grpcMetrics := grpcprometheus.NewServerMetrics(
		grpcprometheus.WithServerCounterOptions(grpcprometheus.WithConstLabels(prometheus.Labels{"server": "iri"})),
		grpcprometheus.WithServerHandlingTimeHistogram(
			// i am not sure, if this buckets are correct
			grpcprometheus.WithHistogramBuckets([]float64{0.01, 0.1, 0.5, 1}),
		),
	)

	err := prometheus.Register(grpcMetrics)
	if err != nil {
		return fmt.Errorf("failed to register iri server metrics: %w", err)
	}

	iriLog := log.WithName("iri-server")

	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			commongrpc.InjectLogger(iriLog),
			commongrpc.LogRequest,
			grpcMetrics.UnaryServerInterceptor(),
			internalutils.RecoveryInterceptor(iriLog, "interceptor"),
		),
		grpc.ConnectionTimeout(opts.ConnectionTimeout),
	)

	iri.RegisterMachineRuntimeServer(grpcSrv, srv)

	setupLog.V(1).Info("Start listening on unix socket", "Address", opts.Addr)
	l, err := net.Listen("unix", opts.Addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer internalutils.Recover(iriLog, "shutdown")
		<-ctx.Done()
		setupLog.Info("Shutting down grpc server")
		grpcSrv.GracefulStop()
		setupLog.Info("GRPC server is shutdown")
	}()
	setupLog.Info("Starting grpc server", "Address", l.Addr().String())
	if err := grpcSrv.Serve(l); err != nil {
		return fmt.Errorf("error serving grpc: %w", err)
	}
	setupLog.Info("GRPC server stopped serving requests")

	wg.Wait()
	return nil
}

func runStreamingServer(ctx context.Context, setupLog, log logr.Logger, srv *server.Server, opts HTTPServerOptions) error {
	serverLog := log.WithName("streaming-server")

	httpHandler, err := console.NewHandler(srv, console.HandlerOptions{
		Log: serverLog,
	})
	if err != nil {
		setupLog.Error(err, "failed to create new streaming handler")
		return err
	}

	httpSrv := &http.Server{
		Addr:         opts.Addr,
		Handler:      httpHandler,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
		IdleTimeout:  opts.IdleTimeout,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer internalutils.Recover(serverLog, "shutdown")
		<-ctx.Done()
		setupLog.Info("Shutting down streaming server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.GracefulTimeout)
		defer cancel()

		locErr := httpSrv.Shutdown(shutdownCtx)
		if locErr != nil {
			setupLog.Error(locErr, "streaming server wasn't shutdown properly")
		} else {
			setupLog.Info("Streaming server is shutdown")
		}
	}()

	setupLog.V(1).Info("Starting streaming server", "Address", opts.Addr)
	if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error listening / serving streaming server: %w", err)
	}
	setupLog.Info("Streaming server stopped serving requests")

	wg.Wait()
	return nil
}

func runMetricsServer(ctx context.Context, setupLog logr.Logger, opts HTTPServerOptions) error {
	if opts.Addr == "" {
		setupLog.Info("Metrics server address isn't configured. Metrics server is disabled.")
		return nil
	}

	setupLog.Info("Starting metrics server on " + opts.Addr)

	serverLog := ctrl.Log.WithName("metrics-server")

	router := chi.NewRouter()
	router.Use(internalutils.RecoveryMiddleware(serverLog, "middleware"))
	router.Handle("/metrics", promhttp.Handler())

	srv := http.Server{
		Addr:         opts.Addr,
		Handler:      router,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
		IdleTimeout:  opts.IdleTimeout,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer internalutils.Recover(serverLog, "shutdown")

		<-ctx.Done()
		setupLog.Info("Shutting down metrics server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.GracefulTimeout)
		defer cancel()

		locErr := srv.Shutdown(shutdownCtx)
		if locErr != nil {
			setupLog.Error(locErr, "metrics server wasn't shutdown properly")
		} else {
			setupLog.Info("Metrics server is shutdown")
		}
	}()

	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error listening / serving metrics server: %w", err)
	}

	setupLog.Info("Metrics server stopped serve new connections")

	wg.Wait()

	return nil
}

func runPPROFServer(ctx context.Context, setupLog, log logr.Logger, opts HTTPServerOptions) error {
	if opts.Addr == "" {
		setupLog.Info("pprof server address isn't configured. pprof server is disabled.")
		return nil
	}

	serverLog := ctrl.Log.WithName("pprof")

	router := chi.NewRouter()
	router.Use(metrics.NewHTTPMetricsMiddlewareHandler(log, "pprof"))
	router.Use(internalutils.RecoveryMiddleware(serverLog, "middleware"))

	router.Get("/debug/pprof/", pprof.Index)
	router.Get("/debug/pprof/cmdline", pprof.Cmdline)
	router.Get("/debug/pprof/profile", pprof.Profile)
	router.Get("/debug/pprof/symbol", pprof.Symbol)
	router.Get("/debug/pprof/trace", pprof.Trace)

	srv := http.Server{
		Addr:         opts.Addr,
		Handler:      router,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
		IdleTimeout:  opts.IdleTimeout,
	}

	setupLog.Info("Starting pprof server on " + opts.Addr)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer internalutils.Recover(serverLog, "shutdown")

		<-ctx.Done()
		setupLog.Info("Shutting down pprof server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.GracefulTimeout)
		defer cancel()

		locErr := srv.Shutdown(shutdownCtx)
		if locErr != nil {
			setupLog.Error(locErr, "pprof server wasn't shutdown properly")
		} else {
			setupLog.Info("pprof server is shutdown")
		}
	}()

	err := srv.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error listening / serving pprof server: %w", err)
	}

	setupLog.Info("pprof server stopped serve new connections")

	wg.Wait()

	return nil
}

func runHealthCheckServer(ctx context.Context, setupLog, log logr.Logger, healthCheck healthcheck.HealthCheck, opts HTTPServerOptions) error {
	serverLog := ctrl.Log.WithName("healthcheck-server")

	router := chi.NewRouter()
	router.Use(metrics.NewHTTPMetricsMiddlewareHandler(log, "healthcheck"))
	router.Use(internalutils.RecoveryMiddleware(serverLog, "middleware"))

	router.Get("/healthz", healthCheck.HealthCheckHandler)
	srv := http.Server{
		Addr:         opts.Addr,
		Handler:      router,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
		IdleTimeout:  opts.IdleTimeout,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer internalutils.Recover(serverLog, "shutdown")

		<-ctx.Done()
		setupLog.Info("Shutting down health check server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.GracefulTimeout)
		defer cancel()

		locErr := srv.Shutdown(shutdownCtx)
		if locErr != nil {
			setupLog.Error(locErr, "health check server wasn't shutdown properly")
		} else {
			setupLog.Info("Health check server is shutdown")
		}
	}()

	setupLog.V(1).Info("Starting health check server", "Address", opts.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("error listening / serving health check server: %w", err)
	}

	wg.Wait()

	return nil
}

func initResourceManager(ctx context.Context, opts sources.Options, machineStore *host.Store[*api.Machine], filename string) error {
	err := manager.ValidateOptions(opts)
	if err != nil {
		return err
	}

	for _, sourceName := range opts.Sources {
		source, err := manager.GetSource(sourceName, opts)
		if err != nil {
			return err
		}

		err = manager.AddSource(source)
		if err != nil {
			return err
		}
	}

	err = manager.SetMachineClassesFilename(filename)
	if err != nil {
		return err
	}

	err = manager.SetVMLimit(opts.VMLimit)
	if err != nil {
		return err
	}

	err = manager.SetLogger(ctrl.Log)
	if err != nil {
		return err
	}

	machines, err := manager.Initialize(ctx, machineStore.List)
	if err != nil {
		return err
	}

	return updateMachinePCIStatus(ctx, machineStore, machines)
}

// updateMachinePCIStatus updates the PCI devices of machines on the store.
// This is to support dynamic change of pci addresses after restart of host machine.
func updateMachinePCIStatus(ctx context.Context, machineStore *host.Store[*api.Machine], machines []*api.Machine) error {
	for _, machine := range machines {
		if _, err := machineStore.Update(ctx, machine); err != nil {
			return fmt.Errorf("failed to update machine: %w", err)
		}
	}
	return nil
}

func initMetrics(ctx context.Context, log logr.Logger, listMachines func(context.Context) ([]*api.Machine, error)) error {
	err := metrics.RegisterAllMetrics()
	if err != nil {
		return fmt.Errorf("failed to register all metrics: %w", err)
	}

	machines, err := listMachines(ctx)
	if err != nil {
		return err
	}

	err = metrics.InitializeMachineMetrics(machines)
	if err != nil {
		return fmt.Errorf("failed to get machinestate metric: %w", err)
	}
	return metrics.InitializeMachineClassesMetrics(log.WithName("machine-classes-metrics"), machines)
}
