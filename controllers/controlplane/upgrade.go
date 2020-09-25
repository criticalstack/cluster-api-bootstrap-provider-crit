package controllers

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"

	controlplanev1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/controlplane/v1alpha2"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal"
)

func (r *CritControlPlaneReconciler) upgradeControlPlane(ctx context.Context, cluster *clusterv1.Cluster, ccp *controlplanev1.CritControlPlane, controlPlane *internal.ControlPlane) (_ ctrl.Result, reterr error) {
	logger := controlPlane.Logger()

	workloadCluster, err := r.managementCluster.GetWorkloadCluster(ctx, util.ObjectKey(cluster))
	if err != nil {
		logger.Error(err, "failed to get remote client for workload cluster", "cluster key", util.ObjectKey(cluster))
		return ctrl.Result{}, err
	}

	status, err := workloadCluster.ClusterStatus(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if int(status.Nodes) <= ccp.Spec.Replicas {
		// scaleUp ensures that we don't continue scaling up while waiting for Machines to have NodeRefs
		return r.scaleUpControlPlane(ctx, cluster, ccp, controlPlane)
	}
	return r.scaleDownControlPlane(ctx, cluster, ccp, controlPlane)
}

func (r *CritControlPlaneReconciler) scaleUpControlPlane(ctx context.Context, cluster *clusterv1.Cluster, ccp *controlplanev1.CritControlPlane, controlPlane *internal.ControlPlane) (ctrl.Result, error) {
	logger := controlPlane.Logger()

	// reconcileHealth returns err if there is a machine being delete which is a required condition to check before scaling up
	if err := r.reconcileHealth(ctx, cluster, ccp, controlPlane); err != nil {
		return ctrl.Result{}, &capierrors.RequeueAfterError{RequeueAfter: healthCheckFailedRequeueAfter}
	}
	bootstrapSpec := ccp.Spec.CritConfigSpec.DeepCopy()
	fd := controlPlane.FailureDomainWithFewestMachines()
	if err := r.cloneConfigsAndGenerateMachine(ctx, cluster, ccp, bootstrapSpec, fd); err != nil {
		logger.Error(err, "Failed to create additional control plane Machine")
		r.recorder.Eventf(ccp, corev1.EventTypeWarning, "FailedScaleUp", "Failed to create additional control plane Machine for cluster %s/%s control plane: %v", cluster.Namespace, cluster.Name, err)
		return ctrl.Result{}, err
	}

	// Requeue the control plane, in case there are other operations to perform
	return ctrl.Result{Requeue: true}, nil
}

func (r *CritControlPlaneReconciler) scaleDownControlPlane(ctx context.Context, cluster *clusterv1.Cluster, ccp *controlplanev1.CritControlPlane, controlPlane *internal.ControlPlane) (ctrl.Result, error) {
	logger := controlPlane.Logger()

	if err := r.reconcileHealth(ctx, cluster, ccp, controlPlane); err != nil {
		return ctrl.Result{}, &capierrors.RequeueAfterError{RequeueAfter: healthCheckFailedRequeueAfter}
	}

	machineToDelete, err := selectMachineForScaleDown(controlPlane)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to select machine for scale down")
	}

	if machineToDelete == nil {
		logger.Info("Failed to pick control plane Machine to delete")
		return ctrl.Result{}, errors.New("failed to pick control plane Machine to delete")
	}

	// NOTE(chrism): Etcd is handled by e2d, so there is no special handling of
	// etcd required. When the Node is available, we implicitly know that etcd
	// is ready. This does require the control plane node to be configured such
	// that the etcd endpoint is only itself (either localhost or the host
	// IPv4).

	if err := r.managementCluster.TargetClusterControlPlaneIsHealthy(ctx, util.ObjectKey(cluster), ccp.Name); err != nil {
		logger.V(2).Info("Waiting for control plane to pass control plane health check before removing a control plane machine", "cause", err)
		r.recorder.Eventf(ccp, corev1.EventTypeWarning, "ControlPlaneUnhealthy",
			"Waiting for control plane to pass control plane health check before removing a control plane machine: %v", err)
		return ctrl.Result{}, &capierrors.RequeueAfterError{RequeueAfter: healthCheckFailedRequeueAfter}

	}
	logger = logger.WithValues("machine", machineToDelete)
	if err := r.Client.Delete(ctx, machineToDelete); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "Failed to delete control plane machine")
		r.recorder.Eventf(ccp, corev1.EventTypeWarning, "FailedScaleDown",
			"Failed to delete control plane Machine %s for cluster %s/%s control plane: %v", machineToDelete.Name, cluster.Namespace, cluster.Name, err)
		return ctrl.Result{}, err
	}

	// Requeue the control plane, in case there are additional operations to perform
	return ctrl.Result{Requeue: true}, nil
}

func selectMachineForScaleDown(controlPlane *internal.ControlPlane) (*clusterv1.Machine, error) {
	machines := controlPlane.Machines
	if needingUpgrade := controlPlane.MachinesNeedingUpgrade(); needingUpgrade.Len() > 0 {
		machines = needingUpgrade
	}
	return controlPlane.MachineInFailureDomainWithMostMachines(machines)
}

func (r *CritControlPlaneReconciler) reconcileHealth(ctx context.Context, cluster *clusterv1.Cluster, ccp *controlplanev1.CritControlPlane, controlPlane *internal.ControlPlane) error {
	log := controlPlane.Logger()

	// Do a health check of the Control Plane components
	if err := r.managementCluster.TargetClusterControlPlaneIsHealthy(ctx, util.ObjectKey(cluster), ccp.Name); err != nil {
		log.V(2).Info("Waiting for control plane to pass control plane health check to continue reconciliation", "cause", err)
		r.recorder.Eventf(ccp, corev1.EventTypeWarning, "ControlPlaneUnhealthy",
			"Waiting for control plane to pass control plane health check to continue reconciliation: %v", err)
		return &capierrors.RequeueAfterError{RequeueAfter: healthCheckFailedRequeueAfter}
	}

	// We need this check for scale up as well as down to avoid scaling up when there is a machine being deleted.
	// This should be at the end of this method as no need to wait for machine to be completely deleted to reconcile etcd.
	// TODO: Revisit during machine remediation implementation which may need to cover other machine phases.
	if controlPlane.HasDeletingMachine() {
		return &capierrors.RequeueAfterError{RequeueAfter: deleteRequeueAfter}
	}
	return nil
}
