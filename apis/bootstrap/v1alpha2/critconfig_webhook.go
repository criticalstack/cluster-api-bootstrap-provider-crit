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

package v1alpha2

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var critconfiglog = logf.Log.WithName("critconfig-resource")

func (r *CritConfig) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-bootstrap-cluster-x-k8s-io-v1alpha2-critconfig,mutating=true,failurePolicy=fail,groups=bootstrap.cluster.x-k8s.io,resources=critconfigs,verbs=create;update,versions=v1alpha2,name=mcritconfig.kb.io

var _ webhook.Defaulter = &CritConfig{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *CritConfig) Default() {
	critconfiglog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:verbs=create;update,path=/validate-bootstrap-cluster-x-k8s-io-v1alpha2-critconfig,mutating=false,failurePolicy=fail,groups=bootstrap.cluster.x-k8s.io,resources=critconfigs,versions=v1alpha2,name=vcritconfig.kb.io

var _ webhook.Validator = &CritConfig{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *CritConfig) ValidateCreate() error {
	critconfiglog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *CritConfig) ValidateUpdate(old runtime.Object) error {
	critconfiglog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *CritConfig) ValidateDelete() error {
	critconfiglog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}
