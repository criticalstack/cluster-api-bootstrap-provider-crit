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
func (src *CritConfig) ConvertTo(dstRaw conversion.Hub) (err error) {
	dst := dstRaw.(*v1alpha2.CritConfig)

	if err := Convert_v1alpha1_CritConfig_To_v1alpha2_CritConfig(src, dst, nil); err != nil {
		return err
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha2) to this version.
func (dst *CritConfig) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1alpha2.CritConfig)

	if err := Convert_v1alpha2_CritConfig_To_v1alpha1_CritConfig(src, dst, nil); err != nil {
		return err
	}
	return nil
}
