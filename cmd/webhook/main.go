package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	agentv1alpha1 "github.com/theakshaypant/yeet/pkg/apis/agent/v1alpha1"
	"github.com/theakshaypant/yeet/pkg/adapter"
	"github.com/theakshaypant/yeet/pkg/provider/github"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(agentv1alpha1.AddToScheme(scheme))
}

func main() {
	var port string
	var namespace string
	flag.StringVar(&port, "port", "8082", "Webhook server port.")
	flag.StringVar(&namespace, "namespace", "yeet-system", "Namespace where the controller and global secrets live.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	logger := ctrl.Log.WithName("webhook")

	config := ctrl.GetConfigOrDie()
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		logger.Error(err, "unable to create kubernetes client")
		os.Exit(1)
	}

	zapLogger := zap.NewRaw(zap.UseDevMode(true))
	sugar := zapLogger.Sugar()

	ghProvider := github.New(sugar)
	a := adapter.New(k8sClient, sugar, namespace, ghProvider)

	mux := http.NewServeMux()
	mux.HandleFunc("/", a.HandleEvent(ctrl.SetupSignalHandler()))
	mux.HandleFunc("/live", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           http.TimeoutHandler(mux, 600*time.Second, "timeout\n"),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	logger.Info("starting webhook server", "port", port)
	if err := srv.ListenAndServe(); err != nil {
		logger.Error(err, "webhook server exited with error")
		os.Exit(1)
	}
}
