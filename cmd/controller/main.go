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

	uberzap "go.uber.org/zap"

	agentv1alpha1 "github.com/theakshaypant/agents-as-code/pkg/apis/agent/v1alpha1"
	agentrun "github.com/theakshaypant/agents-as-code/pkg/reconciler/agentrun"
	repository "github.com/theakshaypant/agents-as-code/pkg/reconciler/repository"
	"github.com/theakshaypant/agents-as-code/pkg/sandbox"
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

	repoReconciler := repository.NewReconciler(mgr.GetClient(), nil)
	if err := repoReconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to set up Repository controller")
		os.Exit(1)
	}

	zapLogger, _ := uberzap.NewProduction()
	sugar := zapLogger.Sugar()
	sb := sandbox.NewStub()
	arReconciler := agentrun.NewReconciler(mgr.GetClient(), sb, agentrun.DefaultProviderFactory, sugar)
	if err := arReconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to set up AgentRun controller")
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
