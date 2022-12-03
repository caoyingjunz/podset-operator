/*
Copyright 2022.

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
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	pixiuv1alpha1 "github.com/caoyingjunz/podset-operator/api/v1alpha1"
	pixiutypes "github.com/caoyingjunz/podset-operator/pkg/types"
	"github.com/caoyingjunz/podset-operator/pkg/util"
)

// PodSetReconciler reconciles a PodSet object
type PodSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=pixiu.pixiu.io,resources=podsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=pixiu.pixiu.io,resources=podsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=pixiu.pixiu.io,resources=podsets/finalizers,verbs=update

// Implement reconcile.Reconciler so the controller can reconcile objects
var _ reconcile.Reconciler = &PodSetReconciler{}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PodSetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("request", req)
	log.V(1).Info("reconciling pod set operator")

	create := false
	name := req.NamespacedName.Name
	podSet := &pixiuv1alpha1.PodSet{}
	if err := r.Get(ctx, req.NamespacedName, podSet); err != nil {
		if apierrors.IsNotFound(err) {
			create = true
			podSet.SetName(name)
		} else {
			log.Error(err, "error requesting pod set operator")
			return reconcile.Result{Requeue: true}, nil
		}
	}

	if create {
		fmt.Println("create", req.Namespace, req.Name)
	} else {
		fmt.Println("update", req.Namespace, req.Name)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueuePod := handler.EnqueueRequestsFromMapFunc(r.mapToPods)

	return ctrl.NewControllerManagedBy(mgr).
		For(&pixiuv1alpha1.PodSet{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, enqueuePod).
		Complete(r)
}

func (r *PodSetReconciler) mapToPods(obj client.Object) (requests []reconcile.Request) {
	if obj == nil {
		return
	}
	if !util.IsOwnedByKind(obj, pixiutypes.PodSetKind) {
		return
	}

	var (
		err     error
		ctx     = context.TODO()
		podSets = &pixiuv1alpha1.PodSetList{}
	)
	// TODO: 追加 label 和 ns 过滤
	if err = r.List(ctx, podSets); err != nil {
		r.Log.Error(err, "failed to list podSet")
		return
	}

	oref := util.GetOwnerByKind(obj, pixiutypes.PodSetKind)
	for _, podSet := range podSets.Items {
		if oref.UID == podSet.UID {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: podSet.Namespace, Name: podSet.Name,
				},
			})
			break
		}
	}

	return
}
