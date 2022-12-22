/*
Copyright 2021 The Pixiu Authors.

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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	validationutils "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var podsetlog = logf.Log.WithName("podset-resource")

func (r *PodSet) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

//+kubebuilder:webhook:path=/mutate-pixiu-pixiu-io-v1alpha1-podset,mutating=true,failurePolicy=fail,sideEffects=None,groups=pixiu.pixiu.io,resources=podsets,verbs=create;update,versions=v1alpha1,name=mpodset.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &PodSet{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *PodSet) Default() {
	podsetlog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-pixiu-pixiu-io-v1alpha1-podset,mutating=false,failurePolicy=fail,sideEffects=None,groups=pixiu.pixiu.io,resources=podsets,verbs=create;update,versions=v1alpha1,name=vpodset.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &PodSet{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *PodSet) ValidateCreate() error {
	podsetlog.Info("validate create", "name", r.Name)

	return r.validatePodSet()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *PodSet) ValidateUpdate(old runtime.Object) error {
	podsetlog.Info("validate update", "name", r.Name)

	return r.validatePodSet()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *PodSet) ValidateDelete() error {
	podsetlog.Info("validate delete", "name", r.Name)

	return nil
}

func (r *PodSet) validatePodSet() error {
	var allErrs field.ErrorList
	if err := r.validatePodSetName(); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := r.validatePodSetSpec(); err != nil {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "pixiu.pixiu.io", Kind: "PodSet"},
		r.Name, allErrs)
}

func (r *PodSet) validatePodSetSpec() *field.Error {
	// TODO
	return nil
}

func (r *PodSet) validatePodSetName() *field.Error {
	if len(r.ObjectMeta.Name) == 0 {
		return field.Invalid(field.NewPath("metadata").Child("name"), r.Name, "must be than 0 characters")
	}
	if len(r.ObjectMeta.Name) > validationutils.DNS1035LabelMaxLength-11 {
		return field.Invalid(field.NewPath("metadata").Child("name"), r.Name, "must be no more than 52 characters")
	}

	return nil
}
