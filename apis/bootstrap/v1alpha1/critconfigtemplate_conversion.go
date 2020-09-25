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
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	"github.com/criticalstack/cluster-api-bootstrap-provider-crit/apis/bootstrap/v1alpha2"
)

// ConvertTo converts this CritConfig to the Hub version (v1alpha2).
func (src *CritConfigTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1alpha2.CritConfigTemplate)

	if err := Convert_v1alpha1_CritConfigTemplate_To_v1alpha2_CritConfigTemplate(src, dst, nil); err != nil {
		return err
	}
	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this version.
func (dst *CritConfigTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1alpha2.CritConfigTemplate)

	if err := Convert_v1alpha2_CritConfigTemplate_To_v1alpha1_CritConfigTemplate(src, dst, nil); err != nil {
		return err
	}
	return nil
}
