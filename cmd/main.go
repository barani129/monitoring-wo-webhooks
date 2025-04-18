/*
Copyright 2024 baranitharan.chittharanjan@spark.co.nz.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	monitoringv1alpha1 "github.com/barani129/monitoring-wo-webhooks/api/v1alpha1"
	"github.com/barani129/monitoring-wo-webhooks/internal/controller"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const inClusterNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(monitoringv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var clusterResourceNamespace string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&clusterResourceNamespace, "cluster-resource-namespace", "", "the namespace for external alert secret")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if clusterResourceNamespace == "" {
		var err error
		clusterResourceNamespace, err = getInClusterNamespace()
		if err != nil {
			if errors.Is(err, errNotInCluster) {
				setupLog.Error(err, "please supply --cluster-resource-namespace")
			} else {
				setupLog.Error(err, "unexpected error while getting in-cluster Namespace")
			}
			os.Exit(1)
		}
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}
	// disable webhook server
	// webhookServer := webhook.NewServer(webhook.Options{
	// 	TLSOpts: tlsOpts,
	// })

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		// WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "b6126cdb.spark.co.nz",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controller.ContainerScanReconciler{
		Kind:                     "ContainerScan",
		Client:                   mgr.GetClient(),
		Scheme:                   mgr.GetScheme(),
		ClusterResourceNamespace: clusterResourceNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ContainerScan")
		os.Exit(1)
	}
	// disabling webhooks
	// if os.Getenv("ENABLE_WEBHOOKS") != "false" {
	// 	if err = (&monitoringv1alpha1.ContainerScan{}).SetupWebhookWithManager(mgr); err != nil {
	// 		setupLog.Error(err, "unable to create webhook", "webhook", "ContainerScan")
	// 		os.Exit(1)
	// 	}
	// }
	if err = (&controller.PortScanReconciler{
		Kind:                     "PortScan",
		Client:                   mgr.GetClient(),
		Scheme:                   mgr.GetScheme(),
		ClusterResourceNamespace: clusterResourceNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PortScan")
		os.Exit(1)
	}
	// disabling webhooks
	// if os.Getenv("ENABLE_WEBHOOKS") != "false" {
	// 	if err = (&monitoringv1alpha1.PortScan{}).SetupWebhookWithManager(mgr); err != nil {
	// 		setupLog.Error(err, "unable to create webhook", "webhook", "PortScan")
	// 		os.Exit(1)
	// 	}
	// }
	if err = (&controller.VmScanReconciler{
		Kind:                     "VmScan",
		Client:                   mgr.GetClient(),
		Scheme:                   mgr.GetScheme(),
		ClusterResourceNamespace: clusterResourceNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "VmScan")
		os.Exit(1)
	}
	// disabling webhooks
	// if os.Getenv("ENABLE_WEBHOOKS") != "false" {
	// 	if err = (&monitoringv1alpha1.VmScan{}).SetupWebhookWithManager(mgr); err != nil {
	// 		setupLog.Error(err, "unable to create webhook", "webhook", "VmScan")
	// 		os.Exit(1)
	// 	}
	// }

	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}

	httpClient := http.Client{}

	restClient, err := apiutil.RESTClientForGVK(gvk, false, mgr.GetConfig(), serializer.NewCodecFactory(mgr.GetScheme()), &httpClient)
	if err != nil {
		setupLog.Error(err, "unable to create REST client")
	}

	if err = (&controller.MetallbScanReconciler{
		Kind:                     "MetallbScan",
		Client:                   mgr.GetClient(),
		RESTClient:               restClient,
		Scheme:                   mgr.GetScheme(),
		RESTConfig:               mgr.GetConfig(),
		ClusterResourceNamespace: clusterResourceNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MetallbScan")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

var errNotInCluster = errors.New("not running in-cluster")

// Copied from controller-runtime/pkg/leaderelection
func getInClusterNamespace() (string, error) {
	// Check whether the namespace file exists.
	// If not, we are not running in cluster so can't guess the namespace.
	_, err := os.Stat(inClusterNamespacePath)
	if os.IsNotExist(err) {
		return "", errNotInCluster
	} else if err != nil {
		return "", fmt.Errorf("error checking namespace file: %w", err)
	}

	// Load the namespace file and return its content
	namespace, err := os.ReadFile(inClusterNamespacePath)
	if err != nil {
		return "", fmt.Errorf("error reading namespace file: %w", err)
	}
	return string(namespace), nil
}
