package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	agentv1alpha1 "github.com/theakshaypant/yeet/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/yeet/pkg/knowledgegraph/graphify"
	repository "github.com/theakshaypant/yeet/pkg/reconciler/repository"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(agentv1alpha1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var healthAddr string
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metrics endpoint binds to.")
	flag.StringVar(&healthAddr, "health-addr", ":8081", "The address the health endpoint binds to.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	logger := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: healthAddr,
	})
	if err != nil {
		logger.Error(err, "unable to create manager")
		os.Exit(1)
	}

	builder := graphify.New("")
	reconciler := repository.NewReconciler(mgr.GetClient(), builder)
	if err := reconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to set up Repository controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	logger.Info("starting controller")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "controller exited with error")
		os.Exit(1)
	}
}
