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

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	controlplanev1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/controlplane/v1alpha2"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal/machinefilters"
)

func (r *CritControlPlaneReconciler) updateStatus(ctx context.Context, ccp *controlplanev1.CritControlPlane, cluster *clusterv1.Cluster) error {
	labelSelector := internal.ControlPlaneSelectorForCluster(cluster.Name, ccp.Name)
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		// Since we are building up the LabelSelector above, this should not fail
		return errors.Wrap(err, "failed to parse label selector")
	}
	// Copy label selector to its status counterpart in string format.
	// This is necessary for CRDs including scale subresources.
	ccp.Status.Selector = selector.String()

	ownedMachines, err := r.managementCluster.GetMachinesForCluster(ctx, util.ObjectKey(cluster), machinefilters.OwnedControlPlaneMachines(ccp.Name))
	if err != nil {
		return errors.Wrap(err, "failed to get list of owned machines")
	}

	currentMachines := ownedMachines.Filter(machinefilters.MatchesConfigurationHash(ccp.Spec.Hash()))
	ccp.Status.UpdatedReplicas = int32(len(currentMachines))

	replicas := int32(len(ownedMachines))

	// set basic data that does not require interacting with the workload cluster
	ccp.Status.Replicas = replicas
	ccp.Status.ReadyReplicas = 0
	ccp.Status.UnavailableReplicas = replicas

	// Return early if the deletion timestamp is set, we don't want to try to
	// connect to the workload cluster.
	if !ccp.DeletionTimestamp.IsZero() {
		return nil
	}

	remoteClient, err := r.remoteClientGetter(ctx, r.Client, util.ObjectKey(cluster), r.Scheme)
	if err != nil {
		return errors.Wrap(err, "failed to create remote cluster client")
	}

	nodes := &corev1.NodeList{}
	if err := remoteClient.List(ctx, nodes, client.MatchingLabels(map[string]string{
		"node-role.kubernetes.io/master": "",
	})); err != nil {
		return err
	}

	readyMachines := int32(0)
	for i := range nodes.Items {
		if util.IsNodeReady(&nodes.Items[i]) {
			readyMachines++
		}
	}

	ccp.Status.ReadyReplicas = readyMachines
	ccp.Status.UnavailableReplicas = replicas - ccp.Status.ReadyReplicas

	// TODO(chrism): Initialized is different for us than defined by kubeadm.
	// We should consider it initialized only after it achieves it's desired
	// size for the first time (vs. kubeadm which is considered initialized
	// after the first ready replica). This is put here for now, but should
	// reflect better what initialized means when using crit.
	if !ccp.Status.Initialized {
		cm := &corev1.ConfigMap{}
		if err := remoteClient.Get(ctx, types.NamespacedName{Name: "crit-config", Namespace: metav1.NamespaceSystem}, cm); err == nil {
			ccp.Status.Initialized = true
		}
	}
	switch ccp.Status.ReadyReplicas {
	case 0:
		ccp.Status.Ready = false
	default:
		ccp.Status.Ready = true
	}
	return nil
}
