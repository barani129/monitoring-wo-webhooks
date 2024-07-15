/*
Copyright 2024 baranitharan.chittharanjan@spark.co.nz.

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
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	validationutils "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var vmscanlog = logf.Log.WithName("vmscan-resource")

// SetupWebhookWithManager will setup the manager to manage the webhooks
func (r *VmScan) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-monitoring-spark-co-nz-v1alpha1-vmscan,mutating=true,failurePolicy=fail,sideEffects=None,groups=monitoring.spark.co.nz,resources=vmscans,verbs=create;update,versions=v1alpha1,name=mvmscan.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &VmScan{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *VmScan) Default() {
	vmscanlog.Info("default", "name", r.Name)

	if r.Spec.SuspendEmailAlert == nil {
		r.Spec.SuspendEmailAlert = new(bool)
		*r.Spec.SuspendEmailAlert = true
	}
	if r.Spec.NotifyExtenal == nil {
		r.Spec.NotifyExtenal = new(bool)
		*r.Spec.NotifyExtenal = false
	}
	if r.Spec.CheckInterval == nil {
		r.Spec.CheckInterval = new(int64)
		*r.Spec.CheckInterval = 2
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-monitoring-spark-co-nz-v1alpha1-vmscan,mutating=false,failurePolicy=fail,sideEffects=None,groups=monitoring.spark.co.nz,resources=vmscans,verbs=create;update,versions=v1alpha1,name=vvmscan.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &VmScan{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *VmScan) ValidateCreate() (admission.Warnings, error) {
	vmscanlog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	return nil, r.ValidateVmScan()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *VmScan) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	vmscanlog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil, r.ValidateVmScan()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *VmScan) ValidateDelete() (admission.Warnings, error) {
	vmscanlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}

func (r *VmScan) ValidateVmScan() error {
	var allErrs field.ErrorList
	if err := r.ValidateVmScanName(); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := r.ValidateVmScanSpec(); err != nil {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(
		schema.GroupKind{Group: "monitoring.spark.co.nz", Kind: "VmScan"},
		r.Name, allErrs)
}

func (r *VmScan) ValidateVmScanSpec() *field.Error {
	if !*r.Spec.SuspendEmailAlert {
		if r.Spec.Email == "" {
			return field.Invalid(field.NewPath("spec").Child("email"), r.Spec.Email, ".spec.email field cannot be empty")
		}
		if r.Spec.RelayHost == "" {
			return field.Invalid(field.NewPath("spec").Child("relayhost"), r.Spec.RelayHost, ".spec.relayHost field cannot be empty")
		}
	}
	if *r.Spec.NotifyExtenal {
		if r.Spec.ExternalSecret == "" {
			return field.Invalid(field.NewPath("spec").Child("externalsecret"), r.Spec.ExternalSecret, ".spec.externalSecret field cannot be empty")
		}
		if r.Spec.ExternalData == "" {
			return field.Invalid(field.NewPath("spec").Child("externaldata"), r.Spec.ExternalData, ".spec.externalData field cannot be empty")
		}
		if !strings.HasPrefix(r.Spec.ExternalURL, "https://") && !strings.HasPrefix(r.Spec.ExternalURL, "http://") {
			return field.Invalid(field.NewPath("spec").Child("externalurl"), r.Spec.ExternalURL, ".spec.external field must start with http:// or https://")
		}
	}
	return nil
}

func (r *VmScan) ValidateVmScanName() *field.Error {
	if len(r.ObjectMeta.Name) > validationutils.DNS1035LabelMaxLength {
		return field.Invalid(field.NewPath("metadata").Child("name"), r.Name, "must be no more than 52 characters")
	}
	return nil
}
