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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pixiuv1alpha1 "github.com/caoyingjunz/podset-operator/api/v1alpha1"
	pixiutypes "github.com/caoyingjunz/podset-operator/pkg/types"
	"github.com/caoyingjunz/podset-operator/pkg/util"
)

func (r *PodSetReconciler) parsePodSelector(ps *pixiuv1alpha1.PodSet) (labels.Selector, error) {
	return metav1.LabelSelectorAsSelector(ps.Spec.Selector)
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
