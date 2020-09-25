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
	"reflect"

	conversion "k8s.io/apimachinery/pkg/conversion"

	bootstrapv1alpha1 "github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha1"
	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/controlplane/v1alpha2"
)

type scope struct{}

func (s *scope) Convert(src, dest interface{}, flags conversion.FieldMatchingFlags) error {
	return nil
}
func (s *scope) SrcTag() reflect.StructTag {
	return ""
}
func (s *scope) DestTag() reflect.StructTag {
	return ""
}
func (s *scope) Flags() conversion.FieldMatchingFlags {
	return 0
}
func (s *scope) Meta() *conversion.Meta {
	return nil
}

func Convert_v1alpha2_CritControlPlaneSpec_To_v1alpha1_CritControlPlaneSpec(in *v1alpha2.CritControlPlaneSpec, out *CritControlPlaneSpec, s conversion.Scope) error {
	if err := autoConvert_v1alpha2_CritControlPlaneSpec_To_v1alpha1_CritControlPlaneSpec(in, out, &scope{}); err != nil {
		return err
	}
	return bootstrapv1alpha1.Convert_v1alpha2_CritConfigSpec_To_v1alpha1_CritConfigSpec(&in.CritConfigSpec, &out.CritConfigSpec, nil)
}

func Convert_v1alpha1_CritControlPlaneSpec_To_v1alpha2_CritControlPlaneSpec(in *CritControlPlaneSpec, out *v1alpha2.CritControlPlaneSpec, s conversion.Scope) error {
	if err := autoConvert_v1alpha1_CritControlPlaneSpec_To_v1alpha2_CritControlPlaneSpec(in, out, &scope{}); err != nil {
		return err
	}
	return bootstrapv1alpha1.Convert_v1alpha1_CritConfigSpec_To_v1alpha2_CritConfigSpec(&in.CritConfigSpec, &out.CritConfigSpec, nil)
}
