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

package controllers

import (
	"context"
	"fmt"

	configutil "github.com/criticalstack/crit/pkg/config/util"
	critv1 "github.com/criticalstack/crit/pkg/config/v1alpha2"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	bsutil "sigs.k8s.io/cluster-api/bootstrap/util"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	"sigs.k8s.io/cluster-api/feature"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	bootstrapv1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha2"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal/remote"
)

// CritConfigReconciler reconciles a CritConfig object
type CritConfigReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	remoteClientGetter remote.ClusterClientGetter
}

func (r *CritConfigReconciler) SetupWithManager(mgr ctrl.Manager, option controller.Options) error {
	if r.remoteClientGetter == nil {
		r.remoteClientGetter = remote.NewClusterClient
	}
	r.Scheme = mgr.GetScheme()

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1.CritConfig{}).
		WithOptions(option).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.MachineToBootstrapMapFunc),
			},
		).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.ClusterToCritConfigs),
			},
		)

	if feature.Gates.Enabled(feature.MachinePool) {
		builder = builder.Watches(
			&source.Kind{Type: &expv1.MachinePool{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.MachinePoolToBootstrapMapFunc),
			},
		)
	}

	if err := builder.Complete(r); err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}
	return nil
}

// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=critconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=critconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status;machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=exp.cluster.x-k8s.io,resources=machinepools;machinepools/status,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets;events;configmaps,verbs=get;list;watch;create;update;patch;delete

func (r *CritConfigReconciler) Reconcile(req ctrl.Request) (res ctrl.Result, reterr error) {
	ctx := context.Background()
	logger := r.Log.WithValues("critconfig", req.NamespacedName)

	// Lookup the crit config
	config := &bootstrapv1.CritConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, config); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get config")
		return ctrl.Result{}, err
	}

	// Look up the owner of this CritConfig if there is one
	configOwner, err := bsutil.GetConfigOwner(ctx, r.Client, config)
	if apierrors.IsNotFound(err) {
		// Could not find the owner yet, this is not an error and will rereconcile when the owner gets set.
		return ctrl.Result{}, nil
	}
	if err != nil {
		logger.Error(err, "failed to get owner")
		return ctrl.Result{}, err
	}
	if configOwner == nil {
		return ctrl.Result{}, nil
	}
	logger = logger.WithValues("kind", configOwner.GetKind(), "version", configOwner.GetResourceVersion(), "name", configOwner.GetName())

	// Lookup the cluster the config owner is associated with
	cluster, err := util.GetClusterByName(ctx, r.Client, configOwner.GetNamespace(), configOwner.ClusterName())
	if err != nil {
		if errors.Cause(err) == util.ErrNoCluster {
			logger.Info(fmt.Sprintf("%s does not belong to a cluster yet, waiting until it's part of a cluster", configOwner.GetKind()))
			return ctrl.Result{}, nil
		}

		if apierrors.IsNotFound(err) {
			logger.Info("cluster does not exist yet, waiting until it is created")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "could not get cluster with metadata")
		return ctrl.Result{}, err
	}

	// Wait for the infrastructure to be ready.
	if !cluster.Status.InfrastructureReady {
		logger.Info("cluster infrastructure is not ready, waiting")
		return ctrl.Result{}, nil
	}

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(config, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile status for machines that already have a secret reference, but our status isn't up to date.
	// This case solves the pivoting scenario (or a backup restore) which doesn't preserve the status subresource on objects.
	if configOwner.DataSecretName() != nil && (!config.Status.Ready || config.Status.DataSecretName == nil) {
		config.Status.Ready = true
		config.Status.DataSecretName = configOwner.DataSecretName()
		return ctrl.Result{}, patchHelper.Patch(ctx, config)
	}

	// Attempt to Patch the CritConfig object and status after each reconciliation if no error occurs.
	defer func() {
		if reterr == nil {
			if reterr = patchHelper.Patch(ctx, config); reterr != nil {
				logger.Error(reterr, "failed to patch config")
			}
		}
	}()

	obj, err := configutil.Unmarshal([]byte(config.Spec.Config))
	if err != nil {
		return ctrl.Result{}, err
	}

	switch cfg := obj.(type) {
	case *critv1.ControlPlaneConfiguration:
		logger.Info("found valid ControlPlaneConfiguration", "cluster", cfg.ClusterName)
		if !configOwner.IsControlPlaneMachine() {
			return ctrl.Result{}, errors.Errorf("config owner is for a worker Machine, but received a ControlPlaneConfiguration")
		}
		return r.reconcileControlPlane(ctx, config, configOwner, cluster, cfg)
	case *critv1.WorkerConfiguration:
		logger.Info("found valid WorkerConfiguration", "cluster", cfg.ClusterName)
		if configOwner.IsControlPlaneMachine() {
			return ctrl.Result{}, errors.Errorf("config owner is for a control plane Machine, but received a WorkerConfiguration")
		}
		return r.reconcileWorker(ctx, config, configOwner, cluster, cfg)
	default:
		return ctrl.Result{}, errors.Errorf("Config %q contained invalid configuration kind: %q", config.Name, obj.GetObjectKind().GroupVersionKind())
	}
}

// MachineToBootstrapMapFunc is a handler.ToRequestsFunc to be used to enqeue
// request for reconciliation of CritConfig.
func (r *CritConfigReconciler) MachineToBootstrapMapFunc(o handler.MapObject) []ctrl.Request {
	m, ok := o.Object.(*clusterv1.Machine)
	if !ok {
		return nil
	}
	result := []ctrl.Request{}
	if m.Spec.Bootstrap.ConfigRef != nil && m.Spec.Bootstrap.ConfigRef.GroupVersionKind() == bootstrapv1.GroupVersion.WithKind("CritConfig") {
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

// MachinePoolToBootstrapMapFunc is a handler.ToRequestsFunc to be used to
// enqueue request for reconciliation of CritConfig.
func (r *CritConfigReconciler) MachinePoolToBootstrapMapFunc(o handler.MapObject) []ctrl.Request {
	m, ok := o.Object.(*expv1.MachinePool)
	if !ok {
		return nil
	}
	result := []ctrl.Request{}
	configRef := m.Spec.Template.Spec.Bootstrap.ConfigRef
	if configRef != nil && configRef.GroupVersionKind().GroupKind() == bootstrapv1.GroupVersion.WithKind("CritConfig").GroupKind() {
		name := client.ObjectKey{Namespace: m.Namespace, Name: configRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

// ClusterToCritConfigs is a handler.ToRequestsFunc to be used to enqeue
// requests for reconciliation of CritConfigs.
func (r *CritConfigReconciler) ClusterToCritConfigs(o handler.MapObject) []ctrl.Request {
	c, ok := o.Object.(*clusterv1.Cluster)
	if !ok {
		r.Log.Error(errors.Errorf("expected a Cluster but got a %T", o.Object), "failed to get Machine for Cluster")
		return nil
	}
	selectors := []client.ListOption{
		client.InNamespace(c.Namespace),
		client.MatchingLabels{
			clusterv1.ClusterLabelName: c.Name,
		},
	}
	machineList := &clusterv1.MachineList{}
	if err := r.Client.List(context.Background(), machineList, selectors...); err != nil {
		r.Log.Error(err, "failed to list Machines", "Cluster", c.Name, "Namespace", c.Namespace)
		return nil
	}
	result := []ctrl.Request{}
	for _, m := range machineList.Items {
		if m.Spec.Bootstrap.ConfigRef != nil &&
			m.Spec.Bootstrap.ConfigRef.GroupVersionKind().GroupKind() == bootstrapv1.GroupVersion.WithKind("CritConfig").GroupKind() {
			name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.Bootstrap.ConfigRef.Name}
			result = append(result, ctrl.Request{NamespacedName: name})
		}
	}
	return result
}
