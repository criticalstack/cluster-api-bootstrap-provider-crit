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

package v1alpha1

import (
	critv1alpha1 "github.com/criticalstack/crit/pkg/config/v1alpha1"
	critv1alpha2 "github.com/criticalstack/crit/pkg/config/v1alpha2"
	"github.com/criticalstack/crit/pkg/kubernetes/yaml"
	"k8s.io/apimachinery/pkg/conversion"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha2"
)

func Convert_v1alpha1_CritConfigSpec_To_v1alpha2_CritConfigSpec(in *CritConfigSpec, out *v1alpha2.CritConfigSpec, s conversion.Scope) error {
	switch {
	case in.ControlPlaneConfiguration != nil:
		data, err := yaml.MarshalToYaml(in.ControlPlaneConfiguration, runtime.NewMultiGroupVersioner(critv1alpha2.SchemeGroupVersion,
			schema.GroupKind{Group: "crit.sh"},
			schema.GroupKind{Group: "crit.criticalstack.com"},
		))
		if err != nil {
			return err
		}
		out.Config = string(data)
	case in.WorkerConfiguration != nil:
		data, err := yaml.MarshalToYaml(in.WorkerConfiguration, runtime.NewMultiGroupVersioner(critv1alpha2.SchemeGroupVersion,
			schema.GroupKind{Group: "crit.sh"},
			schema.GroupKind{Group: "crit.criticalstack.com"},
		))
		if err != nil {
			return err
		}
		out.Config = string(data)
	}
	return autoConvert_v1alpha1_CritConfigSpec_To_v1alpha2_CritConfigSpec(in, out, s)
}

func Convert_v1alpha2_CritConfigSpec_To_v1alpha1_CritConfigSpec(in *v1alpha2.CritConfigSpec, out *CritConfigSpec, s conversion.Scope) error {
	if in.Config != "" {
		obj, err := yaml.UnmarshalFromYaml([]byte(in.Config), runtime.NewMultiGroupVersioner(critv1alpha2.SchemeGroupVersion,
			schema.GroupKind{Group: "crit.sh"},
			schema.GroupKind{Group: "crit.criticalstack.com"},
		))
		if err != nil {
			return err
		}
		switch cfg := obj.(type) {
		case *critv1alpha2.ControlPlaneConfiguration:
			if out.ControlPlaneConfiguration == nil {
				out.ControlPlaneConfiguration = &critv1alpha1.ControlPlaneConfiguration{}
			}
			if err := critv1alpha1.Convert_v1alpha2_ControlPlaneConfiguration_To_v1alpha1_ControlPlaneConfiguration(cfg, out.ControlPlaneConfiguration, nil); err != nil {
				return err
			}
		case *critv1alpha2.WorkerConfiguration:
			if out.WorkerConfiguration == nil {
				out.WorkerConfiguration = &critv1alpha1.WorkerConfiguration{}
			}
			if err := critv1alpha1.Convert_v1alpha2_WorkerConfiguration_To_v1alpha1_WorkerConfiguration(cfg, out.WorkerConfiguration, nil); err != nil {
				return err
			}
		}
	}
	return autoConvert_v1alpha2_CritConfigSpec_To_v1alpha1_CritConfigSpec(in, out, s)
}
