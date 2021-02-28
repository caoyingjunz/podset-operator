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
	"reflect"

	cachev1alpha1 "github.com/caoyingjunz/podset-operator/api/v1alpha1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
	Logger := r.Log.WithValues("podSet", req.NamespacedName)
	Logger.Info("Reconciling podSet")

	// Try to fetch the PodSet
	podSet := &cachev1alpha1.PodSet{}
	err := r.Get(context.TODO(), req.NamespacedName, podSet)
	if err != nil {
		if errors.IsNotFound(err) {
			// Req object not found, Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			// Return and don't requeue
			return ctrl.Result{}, nil
		}
		Logger.Error(err, "failed to get pod from podSet")
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Set the pods owned by PodSet
	lSet := labels.Set{
		"app":      podSet.Name,
		"operator": "podset",
	}

	existingPods := &corev1.PodList{}
	err = r.List(context.TODO(), existingPods, &client.ListOptions{
		Namespace:     req.Namespace,
		LabelSelector: labels.SelectorFromSet(lSet),
	})
	if err != nil {
		Logger.Error(err, "failed to list existing pods in the podSet")
		return ctrl.Result{}, err
	}

	existingPodNames := make([]string, 0)
	for _, pod := range existingPods.Items {
		if pod.GetObjectMeta().GetDeletionTimestamp() != nil {
			continue
		}
		if pod.Status.Phase == corev1.PodPending || pod.Status.Phase == corev1.PodRunning {
			existingPodNames = append(existingPodNames, pod.GetObjectMeta().GetName())
		}
	}
	Logger.Info("Checking podSet", "expected replicas", podSet.Spec.Replicas, "Pod.Names", existingPodNames)

	status := cachev1alpha1.PodSetStatus{
		Replicas: int32(len(existingPodNames)),
		PodNames: existingPodNames,
	}

	if !reflect.DeepEqual(podSet.Status, status) {
		podSet.Status = status
		err := r.Status().Update(context.TODO(), podSet)
		if err != nil {
			Logger.Error(err, "failed to update the podSet")
			return reconcile.Result{}, err
		}
	}

	// Scale Up Pods
	if int32(len(existingPodNames)) < podSet.Spec.Replicas {
		// create a new pod. Just one at a time (this reconciler will be called again afterwards)
		Logger.Info("Adding a pod in the podset", "expected replicas", podSet.Spec.Replicas, "Pod.Names", existingPodNames)

		pod := createPodFromSet(podSet)
		if err := controllerutil.SetControllerReference(podSet, pod, r.Scheme); err != nil {
			Logger.Error(err, "unable to set owner reference on new pod")
			return reconcile.Result{}, err
		}
		err = r.Create(context.TODO(), pod)
		if err != nil {
			Logger.Error(err, "failed to create a pod")
			return reconcile.Result{}, err
		}
	}

	// Scale Down Pods
	if int32(len(existingPodNames)) > podSet.Spec.Replicas {
		// Delete a pod. Just one at a time (this reconciler will be called again afterwards)
		Logger.Info("Deleting a pod in the podset", "expected replicas", podSet.Spec.Replicas, "Pod.Names", existingPodNames)
		// TODO(caoyingjun): 后续优化，删除的应该是最后创建的 pod
		var d int = 0
		for i := 0; i < len(existingPodNames); i++ {
			t1 := existingPods.Items[d].Status.StartTime
			t2 := existingPods.Items[i].Status.StartTime
			if err == nil && t1.Before(t2) {
				t1,d = t2,i
			}
		}
		pod := existingPods.Items[d]
		err = r.Delete(context.TODO(), &pod)
		if err != nil {
			Logger.Error(err, "failed to delete a pod")
			return reconcile.Result{}, err
		}
	}

	return ctrl.Result{Requeue: true}, nil
}

// createPodFromSet returns a test-powerfu pod with the same name/namespace as the set
func createPodFromSet(cr *cachev1alpha1.PodSet) *corev1.Pod {
	labels := map[string]string{
		"app":      cr.Name,
		"operator": "podset",
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: cr.Name + "-pod",
			Namespace:    cr.Namespace,
			Labels:       labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "test-powerful",
					Image:   "jacky06/powerful-tools:v1",
					Command: []string{"sleep", "infinity"},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cachev1alpha1.PodSet{}).
		Complete(r)
}
