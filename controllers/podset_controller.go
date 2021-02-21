/*
Copyright 2021.

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

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cachev1alpha1 "github.com/caoyingjunz/podset-operator/api/v1alpha1"
)

// PodSetReconciler reconciles a PodSet object
type PodSetReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=cache.github.com,resources=podsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cache.github.com,resources=podsets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cache.github.com,resources=podsets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(caoyingjun): Modify the Reconcile function to compare the state specified by
// the PodSet object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *PodSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	Logger := r.Log.WithValues("Podset", req.NamespacedName)
	Logger.Info("Reconciling PodSet")

	// Try to fetch the PodSet
	podSet := &cachev1alpha1.PodSet{}
	err := r.Get(context.TODO(), req.NamespacedName, podSet)
	if err != nil {
		if errors.IsNotFound(err) {
			// Req object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		Logger.Error(err, "failed to get pod from podSet")
		return ctrl.Result{}, err
	}

	// List all pods owned by this PodSet
	lbs := labels.Set{
		"app":     podSet.Name,
		"version": "v0.1",
	}

	existingPods := &corev1.PodList{}
	err = r.List(context.TODO(), existingPods, &client.ListOptions{
		Namespace:     req.Namespace,
		LabelSelector: labels.SelectorFromSet(lbs),
	})
	if err != nil {
		Logger.Error(err, "failed to list existing pods in the podSet")
		return ctrl.Result{}, err
	}

	existingPodNames := []string{}
	for _, pod := range existingPods.Items {
		if pod.GetObjectMeta().GetDeletionTimestamp() != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			existingPodNames = append(existingPodNames, pod.GetObjectMeta().GetName())
		}
	}

	// TODO
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cachev1alpha1.PodSet{}).
		Complete(r)
}
