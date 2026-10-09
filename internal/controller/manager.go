package controller

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/amitpatole/cgroupguard/internal/model"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func emptyReport() model.Report { return model.Report{} }

// Options configure the process (flags in the CLI, values in the Helm chart).
type Options struct {
	MetricsAddr     string
	ProbeAddr       string
	LeaderElect     bool
	LeaderNamespace string
	Config          Config
}

// Run starts the manager and blocks until the context ends.
func Run(ctx context.Context, o Options) error {
	ctrl.SetLogger(logr.FromSlogHandler(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("kubernetes configuration: %w", err)
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metricsserver.Options{BindAddress: o.MetricsAddr},
		HealthProbeBindAddress:  o.ProbeAddr,
		LeaderElection:          o.LeaderElect,
		LeaderElectionID:        "cgroupguard-controller.cgroupguard.io",
		LeaderElectionNamespace: o.LeaderNamespace,
	})
	if err != nil {
		return err
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	r := &Reconciler{Client: mgr.GetClient(), Kube: kube, Recorder: EventsAPI{R: mgr.GetEventRecorder("cgroupguard")}, Cfg: o.Config, Metrics: NewMetrics(DefaultRegistry()), Scanner: NewLiveScanner(kube, o.Config)}
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(GVK)
	if err := ctrl.NewControllerManagedBy(mgr).For(obj).Named("cgroupreadiness").Complete(r); err != nil {
		return err
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}
	return mgr.Start(ctx)
}
