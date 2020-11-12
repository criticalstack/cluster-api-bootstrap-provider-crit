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
	"github.com/onsi/gomega/types"
	"testing"

	. "github.com/onsi/gomega"

	"k8s.io/utils/pointer"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
)

func TestNewFailureDomainPicker(t *testing.T) {
	a := pointer.StringPtr("us-west-1a")
	b := pointer.StringPtr("us-west-1b")

	fds := clusterv1.FailureDomains{
		*a: clusterv1.FailureDomainSpec{},
		*b: clusterv1.FailureDomainSpec{},
	}
	machinea := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: a}}
	machineb := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: b}}
	machinenil := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: nil}}

	testcases := []struct {
		name     string
		fds      clusterv1.FailureDomains
		machines FilterableMachineCollection
		expected []*string
	}{
		{
			name:     "simple",
			expected: nil,
		},
		{
			name: "no machines",
			fds: clusterv1.FailureDomains{
				*a: clusterv1.FailureDomainSpec{},
			},
			expected: []*string{a},
		},
		{
			name:     "one machine in a failure domain",
			fds:      fds,
			machines: NewFilterableMachineCollection(machinea.DeepCopy()),
			expected: []*string{b},
		},
		{
			name: "no failure domain specified on machine",
			fds: clusterv1.FailureDomains{
				*a: clusterv1.FailureDomainSpec{},
			},
			machines: NewFilterableMachineCollection(machinenil.DeepCopy()),
			expected: []*string{a},
		},
		{
			name: "mismatched failure domain on machine",
			fds: clusterv1.FailureDomains{
				*a: clusterv1.FailureDomainSpec{},
			},
			machines: NewFilterableMachineCollection(machineb.DeepCopy()),
			expected: []*string{a},
		},
		{
			name:     "failure domains and no machines should return a valid failure domain",
			fds:      fds,
			expected: []*string{a, b},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			fd := PickFewest(tc.fds, tc.machines)
			if tc.expected == nil {
				g.Expect(fd).To(BeNil())
			} else {
				g.Expect(fd).To(BeElementOf(tc.expected))
			}
		})
	}
}

func TestNewFailureDomainPickNFewest(t *testing.T) {
	a := pointer.StringPtr("us-west-1a")
	b := pointer.StringPtr("us-west-1b")
	c := pointer.StringPtr("us-west-1c")
	fds := clusterv1.FailureDomains{
		*a: clusterv1.FailureDomainSpec{},
		*b: clusterv1.FailureDomainSpec{},
		*c: clusterv1.FailureDomainSpec{},
	}
	machinea := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: a}}
	machineb := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: b}}
	machinec := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: c}}
	machinenil := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: nil}}

	testcases := []struct {
		name     string
		fds      clusterv1.FailureDomains
		machines FilterableMachineCollection
		n        int
		expected types.GomegaMatcher
	}{
		{
			name:     "simple",
			n:        1,
			expected: Equal([]*string{nil}),
		},
		{
			name:     "simple 3",
			n:        3,
			expected: Equal([]*string{nil, nil, nil}),
		},
		{
			name: "machines and no failure domain",
			machines: NewFilterableMachineCollection(
				machinea.DeepCopy(),
				machineb.DeepCopy(),
				machinec.DeepCopy()),
			n:        3,
			expected: Equal([]*string{nil, nil, nil}),
		},
		{
			name: "failure domains and no machines",
			fds:  fds,
			n:    2,
			expected: Or(
				ConsistOf([]*string{a, b}),
				ConsistOf([]*string{b, c}),
				ConsistOf([]*string{c, a}),
			),
		},
		{
			name: "machines in all but one failure domain",
			fds:  fds,
			machines: NewFilterableMachineCollection(
				machinea.DeepCopy(),
				machineb.DeepCopy()),
			n: 2,
			expected: Or(
				ConsistOf([]*string{c, b}),
				ConsistOf([]*string{c, a}),
			),
		},
		{
			name:     "n greater than number of failure domains",
			fds:      fds,
			n:        6,
			expected: ConsistOf([]*string{a, b, c, a, b, c}),
		},
		{
			name: "no failure domain specified on machine",
			fds: clusterv1.FailureDomains{
				*a: clusterv1.FailureDomainSpec{},
			},
			machines: NewFilterableMachineCollection(machinenil.DeepCopy()),
			n:        3,
			expected: Equal([]*string{a, a, a}),
		},
		{
			name: "mismatched failure domain on machine",
			fds: clusterv1.FailureDomains{
				*a: clusterv1.FailureDomainSpec{},
			},
			machines: NewFilterableMachineCollection(
				machineb.DeepCopy(),
				machinec.DeepCopy(),
			),
			n:        3,
			expected: Equal([]*string{a, a, a}),
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			fds := PickNFewest(tc.fds, tc.machines, tc.n)
			if tc.expected == nil {
				g.Expect(fds).To(BeNil())
			} else {
				g.Expect(fds).To(tc.expected)
				g.Expect(len(fds)).To(Equal(tc.n))
			}
		})
	}
}

func TestNewFailureDomainPickMost(t *testing.T) {
	a := pointer.StringPtr("us-west-1a")
	b := pointer.StringPtr("us-west-1b")

	fds := clusterv1.FailureDomains{
		*a: clusterv1.FailureDomainSpec{ControlPlane: true},
		*b: clusterv1.FailureDomainSpec{ControlPlane: true},
	}
	machinea := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: a}}
	machineb := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: b}}
	machinenil := &clusterv1.Machine{Spec: clusterv1.MachineSpec{FailureDomain: nil}}

	testcases := []struct {
		name     string
		fds      clusterv1.FailureDomains
		machines FilterableMachineCollection
		expected []*string
	}{
		{
			name:     "simple",
			expected: nil,
		},
		{
			name: "no machines should return nil",
			fds: clusterv1.FailureDomains{
				*a: clusterv1.FailureDomainSpec{},
			},
			expected: nil,
		},
		{
			name:     "one machine in a failure domain",
			fds:      fds,
			machines: NewFilterableMachineCollection(machinea.DeepCopy()),
			expected: []*string{a},
		},
		{
			name: "no failure domain specified on machine",
			fds: clusterv1.FailureDomains{
				*a: clusterv1.FailureDomainSpec{ControlPlane: true},
			},
			machines: NewFilterableMachineCollection(machinenil.DeepCopy()),
			expected: nil,
		},
		{
			name: "mismatched failure domain on machine should return nil",
			fds: clusterv1.FailureDomains{
				*a: clusterv1.FailureDomainSpec{ControlPlane: true},
			},
			machines: NewFilterableMachineCollection(machineb.DeepCopy()),
			expected: nil,
		},
		{
			name:     "failure domains and no machines should return nil",
			fds:      fds,
			expected: nil,
		},
		{
			name:     "nil failure domains with machines",
			machines: NewFilterableMachineCollection(machineb.DeepCopy()),
			expected: nil,
		},
		{
			name:     "nil failure domains with no machines",
			expected: nil,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			fd := PickMost(&ControlPlane{Machines: tc.machines,
				Cluster: &clusterv1.Cluster{Status: clusterv1.ClusterStatus{FailureDomains: tc.fds}}}, tc.machines)
			if tc.expected == nil {
				g.Expect(fd).To(BeNil())
			} else {
				g.Expect(fd).To(BeElementOf(tc.expected))
			}
		})
	}
}
