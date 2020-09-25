/*
Copyright 2020 Critical Stack, LLC

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
	"flag"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/rest"
	clusterv1alpha3 "sigs.k8s.io/cluster-api/api/v1alpha3"
	expv1alpha3 "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	bootstrapv1alpha1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha1"
	bootstrapv1alpha2 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha2"
	controlplanev1alpha1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/controlplane/v1alpha1"
	controlplanev1alpha2 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/controlplane/v1alpha2"
	controllerbootstrap "github.com/criticalstack/cluster-api-bootstrap-provider-crit/controllers/bootstrap"
	controllercontrolplane "github.com/criticalstack/cluster-api-bootstrap-provider-crit/controllers/controlplane"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = clusterv1alpha3.AddToScheme(scheme)
	_ = expv1alpha3.AddToScheme(scheme)
	_ = bootstrapv1alpha1.AddToScheme(scheme)
	_ = controlplanev1alpha1.AddToScheme(scheme)

	_ = bootstrapv1alpha2.AddToScheme(scheme)
	_ = controlplanev1alpha2.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

var (
	metricsAddr                 string
	enableLeaderElection        bool
	leaderElectionLeaseDuration time.Duration
	leaderElectionRenewDeadline time.Duration
	leaderElectionRetryPeriod   time.Duration
	watchNamespace              string
	profilerAddress             string
	critConfigConcurrency       int
	critControlPlaneConcurrency int
	syncPeriod                  time.Duration
)

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var webhookPort int
	flag.StringVar(&metricsAddr, "metrics-addr", ":8081", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.DurationVar(&leaderElectionLeaseDuration, "leader-election-lease-duration", 15*time.Second,
		"Interval at which non-leader candidates will wait to force acquire leadership (duration string)")
	flag.DurationVar(&leaderElectionRenewDeadline, "leader-election-renew-deadline", 10*time.Second,
		"Duration that the acting master will retry refreshing leadership before giving up (duration string)")
	flag.DurationVar(&leaderElectionRetryPeriod, "leader-election-retry-period", 2*time.Second,
		"Duration the LeaderElector clients should wait between tries of actions (duration string)")
	flag.StringVar(&watchNamespace, "namespace", "",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.")
	flag.StringVar(&profilerAddress, "profiler-address", "",
		"Bind address to expose the pprof profiler (e.g. localhost:6060)")
	flag.IntVar(&critConfigConcurrency, "critconfig-concurrency", 10,
		"Number of crit configs to process simultaneously")
	flag.IntVar(&critControlPlaneConcurrency, "critcontrolplane-concurrency", 10,
		"Number of crit control planes to process simultaneously")
	flag.DurationVar(&syncPeriod, "sync-period", 10*time.Minute,
		"The minimum interval at which watched resources are reconciled (e.g. 15m)")
	flag.IntVar(&webhookPort, "webhook-port", 0,
		"Webhook Server port, disabled by default. When enabled, the manager will only work as webhook server, no reconcilers are installed.")

	flag.Parse()

	ctrl.SetLogger(zap.New(func(o *zap.Options) {
		o.Development = true
	}))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "controller-leader-election-cabpc",
		LeaseDuration:      &leaderElectionLeaseDuration,
		RenewDeadline:      &leaderElectionRenewDeadline,
		RetryPeriod:        &leaderElectionRetryPeriod,
		Namespace:          watchNamespace,
		SyncPeriod:         &syncPeriod,
		Port:               9443,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if webhookPort != 0 {
		if err = (&bootstrapv1alpha2.CritConfig{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CritConfig")
			os.Exit(1)
		}
		if err = (&controlplanev1alpha2.CritControlPlane{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CritControlPlane")
			os.Exit(1)
		}
	} else {
		if err = (&controllerbootstrap.CritConfigReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("CritConfig"),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: critConfigConcurrency}); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "CritConfig")
			os.Exit(1)
		}
		if err = (&controllercontrolplane.CritControlPlaneReconciler{
			Client: mgr.GetClient(),
			Log:    ctrl.Log.WithName("controllers").WithName("CritControlPlane"),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: critControlPlaneConcurrency}); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "CritControlPlane")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// TODO(chrism): this was copied from cluster-api, need to evaluate all of the
// implications of it's usage before enabling
// newClientFunc returns a client reads from cache and write directly to the server
// this avoid get unstructured object directly from the server
// see issue: https://github.com/kubernetes-sigs/cluster-api/issues/1663
func newClientFunc(cache cache.Cache, config *rest.Config, options client.Options) (client.Client, error) {
	// Create the Client for Write operations.
	c, err := client.New(config, options)
	if err != nil {
		return nil, err
	}

	return &client.DelegatingClient{
		Reader:       cache,
		Writer:       c,
		StatusClient: c,
	}, nil
}
