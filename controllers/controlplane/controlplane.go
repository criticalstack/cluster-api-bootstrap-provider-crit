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
	"path/filepath"
	"strings"

	critv1 "github.com/criticalstack/crit/pkg/config/v1alpha2"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/controllers/external"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	controlplanev1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/controlplane/v1alpha2"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal/machinefilters"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal/secret"
)

func (r *CritControlPlaneReconciler) reconcileControlPlane(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	ccp *controlplanev1.CritControlPlane,
	cfg *critv1.ControlPlaneConfiguration,
) (res ctrl.Result, reterr error) {
	logger := r.Log.WithValues("namespace", ccp.Namespace, "critControlPlane", ccp.Name, "cluster", cluster.Name)
	logger.Info("Reconcile CritControlPlane")

	// if object doesn't have a finalizer, add one
	controllerutil.AddFinalizer(ccp, controlplanev1.CritControlPlaneFinalizer)

	// make sure to reconcile the external infrastructure reference
	if err := r.reconcileExternalReference(ctx, cluster, ccp.Spec.InfrastructureTemplate); err != nil {
		return ctrl.Result{}, err
	}

	// generate Cluster Certificates if needed
	if err := secret.NewCertificatesForControlPlane(filepath.Join(cfg.NodeConfiguration.KubeDir, "pki")).LookupOrGenerate(
		ctx,
		r.Client,
		util.ObjectKey(cluster),
		*metav1.NewControllerRef(ccp, controlplanev1.GroupVersion.WithKind("CritControlPlane")),
	); err != nil {
		logger.Error(err, "unable to lookup or create cluster certificates")
		return ctrl.Result{}, err
	}

	// if ControlPlaneEndpoint is not set, return early
	if cluster.Spec.ControlPlaneEndpoint.IsZero() {
		logger.Info("cluster does not yet have a ControlPlaneEndpoint defined")
		return ctrl.Result{}, nil
	}

	// generate cluster Kubeconfig if needed
	if err := r.reconcileControlPlaneKubeconfig(ctx, cluster, ccp); err != nil {
		logger.Error(err, "failed to reconcile Kubeconfig")
		return ctrl.Result{}, err
	}

	ownedMachines, err := r.managementCluster.GetMachinesForCluster(ctx, util.ObjectKey(cluster), machinefilters.OwnedControlPlaneMachines(ccp.Name))
	if err != nil {
		logger.Error(err, "failed to retrieve control plane machines for cluster")
		return ctrl.Result{}, err
	}
	controlPlane := internal.NewControlPlane(cluster, ccp, ownedMachines, ctx, r.Client)
	if err := controlPlane.EnsureOwnership(); err != nil {
		logger.Error(err, "failed to ensure ownership of control plane machines for cluster")
		return ctrl.Result{}, err
	}

	// TODO(chrism): If the control plane did not get initialized, then an
	// upgrade will not be possible and it should fall through to finish
	// initialization or allow a timeout failure (once implemented).
	if len(controlPlane.MachinesNeedingUpgrade()) > 0 {
		logger.Info("Upgrading Control Plane")
		return r.upgradeControlPlane(ctx, cluster, ccp, controlPlane)
	}

	// TODO(chrism): An additional field should be added to indicate if the
	// CritControlPlane is colocated with etcd. In the case it is, the webhook
	// should prevent modification of replicas, and require values 1, 3 or 5,
	// and finally the field should be immutable. If the CritControlPlane is
	// not colocated with etcd, these checks will not apply.
	if len(ownedMachines) == ccp.Spec.Replicas {
		return ctrl.Result{}, nil
	}

	if len(ownedMachines) == 0 {
		logger.Info("Initializing control plane", "Replicas", ccp.Spec.Replicas, "Existing", len(ownedMachines))
		bootstrapSpec := ccp.Spec.CritConfigSpec.DeepCopy()

		// TODO(chrism): replace with NFailureDomainsWithFewestMachines so that
		// created control plane will be spread out over failure domains
		fd := controlPlane.FailureDomainWithFewestMachines()
		for i := 0; i < ccp.Spec.Replicas; i++ {
			if err := r.cloneConfigsAndGenerateMachine(ctx, cluster, ccp, bootstrapSpec, fd); err != nil {
				logger.Error(err, "failed to create initial control plane Machine")
				r.recorder.Eventf(ccp, corev1.EventTypeWarning, "FailedInitialization", "Failed to create initial control plane Machine for cluster %s/%s control plane: %v", cluster.Namespace, cluster.Name, err)
				return ctrl.Result{}, err
			}
		}

		// requeue the control plane, in case there are additional operations to perform
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

func (r *CritControlPlaneReconciler) reconcileExternalReference(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	ref corev1.ObjectReference,
) error {
	if !strings.HasSuffix(ref.Kind, external.TemplateSuffix) {
		return nil
	}
	obj, err := external.Get(ctx, r.Client, &ref, cluster.Namespace)
	if err != nil {
		return err
	}

	// Note: We intentionally do not handle checking for the paused label on an
	// external template reference

	patchHelper, err := patch.NewHelper(obj, r.Client)
	if err != nil {
		return err
	}
	obj.SetOwnerReferences(util.EnsureOwnerRef(obj.GetOwnerReferences(), metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cluster.Name,
		UID:        cluster.UID,
	}))
	return patchHelper.Patch(ctx, obj)
}

func (r *CritControlPlaneReconciler) reconcileControlPlaneKubeconfig(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	ccp *controlplanev1.CritControlPlane,
) error {
	if _, err := secret.GetFromNamespacedName(ctx, r.Client, util.ObjectKey(cluster), secret.Kubeconfig); err != nil {
		if apierrors.IsNotFound(err) {
			if err := kubeconfig.CreateSecretWithOwner(
				ctx,
				r.Client,
				util.ObjectKey(cluster),
				cluster.Spec.ControlPlaneEndpoint.String(),
				*metav1.NewControllerRef(ccp, controlplanev1.GroupVersion.WithKind("CritControlPlane")),
			); err != nil {
				if err == kubeconfig.ErrDependentCertificateNotFound {
					return errors.Wrapf(&capierrors.RequeueAfterError{RequeueAfter: DependentCertRequeueAfter},
						"could not find secret %q for Cluster %q in namespace %q, requeuing",
						secret.ClusterCA, cluster.GetName(), cluster.GetNamespace())
				}
				return err
			}
		}
		return errors.Wrapf(err, "failed to retrieve Kubeconfig Secret for Cluster %q in namespace %q", cluster.GetName(), cluster.GetNamespace())
	}
	return nil
}
