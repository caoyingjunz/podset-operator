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

package controllers

import (
	"context"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pixiuv1alpha1 "github.com/caoyingjunz/podset-operator/api/v1alpha1"
	pixiutypes "github.com/caoyingjunz/podset-operator/pkg/types"
)

func (r *PodSetReconciler) mapToPods(obj client.Object) (requests []reconcile.Request) {
	if obj == nil {
		return
	}

	// If it has a ControllerRef, that's all that matters.
	if controllerRef := metav1.GetControllerOf(obj); controllerRef != nil {
		podSet := r.resolveControllerRef(obj.GetNamespace(), controllerRef)
		if podSet == nil {
			return
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: podSet.Namespace, Name: podSet.Name,
			},
		})
	}

	return
}

func (r *PodSetReconciler) parsePodSelector(ps *pixiuv1alpha1.PodSet) (labels.Selector, error) {
	return metav1.LabelSelectorAsSelector(ps.Spec.Selector)
}

func (r *PodSetReconciler) resolveControllerRef(namespace string, controllerRef *metav1.OwnerReference) *pixiuv1alpha1.PodSet {
	if controllerRef.Kind != pixiutypes.PodSetKind {
		return nil
	}

	podSet := &pixiuv1alpha1.PodSet{}
	if err := r.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: controllerRef.Name}, podSet); err != nil {
		return nil
	}
	if podSet.UID != controllerRef.UID {
		return nil
	}

	return podSet
}

func IsPodActive(p *v1.Pod) bool {
	return v1.PodSucceeded != p.Status.Phase &&
		v1.PodFailed != p.Status.Phase &&
		p.DeletionTimestamp == nil
}

// FilterActivePods returns pods that have not terminated.
func FilterActivePods(pods []v1.Pod) []*v1.Pod {
	var result []*v1.Pod
	for _, p := range pods {
		if IsPodActive(&p) {
			result = append(result, &p)
		}
	}
	return result
}
