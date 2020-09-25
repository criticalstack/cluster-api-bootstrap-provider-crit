/*
Copyright 2020 The Kubernetes Authors.

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

package internal

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/storage/names"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	bootstrapv1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha2"
	controlplanev1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/controlplane/v1alpha2"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/internal/machinefilters"
)

// ControlPlane holds business logic around control planes.
// It should never need to connect to a service, that responsibility lies outside of this struct.
type ControlPlane struct {
	CCP      *controlplanev1.CritControlPlane
	Cluster  *clusterv1.Cluster
	Machines FilterableMachineCollection
	Client   ctrlclient.Client
	ctx      context.Context
}

// NewControlPlane returns an instantiated ControlPlane.
func NewControlPlane(cluster *clusterv1.Cluster, ccp *controlplanev1.CritControlPlane, ownedMachines FilterableMachineCollection, ctx context.Context, client ctrlclient.Client) *ControlPlane {
	return &ControlPlane{
		CCP:      ccp,
		Cluster:  cluster,
		Machines: ownedMachines,
		Client:   client,
		ctx:      ctx,
	}
}

// Logger returns a logger with useful context.
func (c *ControlPlane) Logger() logr.Logger {
	return Log.WithValues("namespace", c.CCP.Namespace, "name", c.CCP.Name, "cluster-name", c.Cluster.Name)
}

// FailureDomains returns a slice of failure domain objects synced from the infrastructure provider into Cluster.Status.
func (c *ControlPlane) FailureDomains() clusterv1.FailureDomains {
	if c.Cluster.Status.FailureDomains == nil {
		return clusterv1.FailureDomains{}
	}
	return c.Cluster.Status.FailureDomains
}

// Version returns the CritControlPlane's version.
func (c *ControlPlane) Version() *string {
	return &c.CCP.Spec.Version
}

// InfrastructureTemplate returns the CritControlPlane's infrastructure template.
func (c *ControlPlane) InfrastructureTemplate() *corev1.ObjectReference {
	return &c.CCP.Spec.InfrastructureTemplate
}

// SpecHash returns the hash of the CritControlPlane spec.
func (c *ControlPlane) SpecHash() string {
	return c.CCP.Spec.Hash()
}

func (c *ControlPlane) EnsureOwnership() error {
	var errs []error
	for _, machine := range c.Machines {
		c.Logger().Info("Checking ownership of machine", "CritControlPlane", c.CCP.Name, "Machine", machine.Name)
		controllerRef := metav1.GetControllerOf(machine)
		if controllerRef != nil {
			if controllerRef.Kind != "CritControlPlane" || controllerRef.Name != c.CCP.Name {
				return fmt.Errorf("Machine %s is already owned by another controller", machine.Name)
			}
		} else {
			c.Logger().Info("Taking ownership of machine", "CritControlPlane", c.CCP.Name, "Machine", machine.Name)
			machine.OwnerReferences = append(machine.OwnerReferences,
				*metav1.NewControllerRef(c.CCP, controlplanev1.GroupVersion.WithKind("CritControlPlane")))
			if err := c.Client.Update(c.ctx, machine); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return kerrors.NewAggregate(errs)
	}
	return nil
}

// AsOwnerReference returns an owner reference to the CritControlPlane.
func (c *ControlPlane) AsOwnerReference() *metav1.OwnerReference {
	return &metav1.OwnerReference{
		APIVersion: controlplanev1.GroupVersion.String(),
		Kind:       "CritControlPlane",
		Name:       c.CCP.Name,
		UID:        c.CCP.UID,
	}
}

// MachinesNeedingUpgrade return a list of machines that need to be upgraded.
func (c *ControlPlane) MachinesNeedingUpgrade() FilterableMachineCollection {
	// NOTE(chrism): this is omitting UpgradeAfter
	return c.Machines.Filter(
		machinefilters.Not(machinefilters.MatchesConfigurationHash(c.SpecHash())),
	)
}

// MachineInFailureDomainWithMostMachines returns the first matching failure domain with machines that has the most control-plane machines on it.
func (c *ControlPlane) MachineInFailureDomainWithMostMachines(machines FilterableMachineCollection) (*clusterv1.Machine, error) {
	fd := c.FailureDomainWithMostMachines(machines)
	machinesInFailureDomain := machines.Filter(machinefilters.InFailureDomains(fd))
	machineToMark := machinesInFailureDomain.Oldest()
	if machineToMark == nil {
		return nil, errors.New("failed to pick control plane Machine to mark for deletion")
	}
	return machineToMark, nil
}

// FailureDomainWithMostMachines returns a fd which exists both in machines and control-plane machines and has the most
// control-plane machines on it.
func (c *ControlPlane) FailureDomainWithMostMachines(machines FilterableMachineCollection) *string {
	// See if there are any Machines that are not in currently defined failure domains first.
	notInFailureDomains := machines.Filter(
		machinefilters.Not(machinefilters.InFailureDomains(c.FailureDomains().FilterControlPlane().GetIDs()...)),
	)
	if len(notInFailureDomains) > 0 {
		// return the failure domain for the oldest Machine not in the current list of failure domains
		// this could be either nil (no failure domain defined) or a failure domain that is no longer defined
		// in the cluster status.
		return notInFailureDomains.Oldest().Spec.FailureDomain
	}

	return PickMost(c, machines)
}

// FailureDomainWithFewestMachines returns the failure domain with the fewest number of machines.
// Used when scaling up.
func (c *ControlPlane) FailureDomainWithFewestMachines() *string {
	if len(c.Cluster.Status.FailureDomains.FilterControlPlane()) == 0 {
		return nil
	}
	return PickFewest(c.FailureDomains().FilterControlPlane(), c.Machines)
}

// GenerateCritConfig generates a new crit config for creating new control plane nodes.
func (c *ControlPlane) GenerateCritConfig(spec *bootstrapv1.CritConfigSpec) *bootstrapv1.CritConfig {
	// Create an owner reference without a controller reference because the owning controller is the machine controller
	owner := metav1.OwnerReference{
		APIVersion: controlplanev1.GroupVersion.String(),
		Kind:       "CritControlPlane",
		Name:       c.CCP.Name,
		UID:        c.CCP.UID,
	}

	bootstrapConfig := &bootstrapv1.CritConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.SimpleNameGenerator.GenerateName(c.CCP.Name + "-"),
			Namespace:       c.CCP.Namespace,
			Labels:          ControlPlaneLabelsForClusterWithHash(c.Cluster.Name, c.CCP.Name, c.SpecHash()),
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Spec: *spec,
	}
	return bootstrapConfig
}

// NewMachine returns a machine configured to be a part of the control plane.
func (c *ControlPlane) NewMachine(infraRef, bootstrapRef *corev1.ObjectReference, failureDomain *string) *clusterv1.Machine {
	return &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName(c.CCP.Name + "-"),
			Namespace: c.CCP.Namespace,
			Labels:    ControlPlaneLabelsForClusterWithHash(c.Cluster.Name, c.CCP.Name, c.SpecHash()),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(c.CCP, controlplanev1.GroupVersion.WithKind("CritControlPlane")),
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName:       c.Cluster.Name,
			Version:           c.Version(),
			InfrastructureRef: *infraRef,
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: bootstrapRef,
			},
			FailureDomain: failureDomain,
		},
	}
}

// HasDeletingMachine returns true if any machine in the control plane is in the process of being deleted.
func (c *ControlPlane) HasDeletingMachine() bool {
	return len(c.Machines.Filter(machinefilters.HasDeletionTimestamp)) > 0
}
