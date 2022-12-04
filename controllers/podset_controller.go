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
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	pixiuv1alpha1 "github.com/caoyingjunz/podset-operator/api/v1alpha1"
	"github.com/caoyingjunz/podset-operator/pkg/types"
)

// PodSetReconciler reconciles a PodSet object
type PodSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger

	Recorder record.EventRecorder // TODO
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

	podSet := &pixiuv1alpha1.PodSet{}
	if err := r.Get(ctx, req.NamespacedName, podSet); err != nil {
		if apierrors.IsNotFound(err) {
			// Req object not found, Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		} else {
			log.Error(err, "error requesting pod set operator")
			// Error reading the object - requeue the request.
			return reconcile.Result{Requeue: true}, nil
		}
	}

	labelSelector, err := r.parsePodSelector(podSet)
	if err != nil {
		return reconcile.Result{Requeue: true}, nil
	}
	allPods := &corev1.PodList{}
	// list all pods to include the pods that don't match the rs`s selector anymore but has the stale controller ref.
	if err = r.List(ctx, allPods, &client.ListOptions{Namespace: req.Namespace, LabelSelector: labelSelector}); err != nil {
		log.Error(err, "error list pods")
		return reconcile.Result{Requeue: true}, nil
	}
	// Ignore inactive pods.
	filteredPods := FilterActivePods(allPods.Items)

	var replicasErr error
	if podSet.DeletionTimestamp == nil {
		replicasErr = r.manageReplicas(ctx, filteredPods, podSet)
	}

	podSet = podSet.DeepCopy()
	newStatus := r.calculateStatus(podSet, filteredPods, replicasErr)

	fmt.Println("pods", newStatus)
	return ctrl.Result{}, nil
}

func (r *PodSetReconciler) manageReplicas(ctx context.Context, filteredPods []*corev1.Pod, podSet *pixiuv1alpha1.PodSet) error {
	diff := len(filteredPods) - int(*podSet.Spec.Replicas)
	if diff < 0 {
		// 实际的pod少于期望，新增 pod
		diff *= -1
		if diff > types.BurstReplicas {
			diff = types.BurstReplicas
		}
		r.Log.Info("Too few replicas", "podSet", klog.KObj(podSet), "need", *(podSet.Spec.Replicas), "creating", diff)
		_, err := r.createPodsInBatch(diff, 1, func() error {
			if err := r.createPod(ctx, podSet.Namespace, &podSet.Spec.Template, podSet, metav1.NewControllerRef(podSet, pixiuv1alpha1.GroupVersionKind)); err != nil {
				fmt.Println("error", err)
				return err
			}
			return nil
		})

		return err

	} else if diff > 0 {
		// 实际存在的pod大于期望值，删除多余的 pod
		fmt.Println("删除pod", diff)
	}

	return nil
}

func (r *PodSetReconciler) createPod(ctx context.Context, namespace string, template *corev1.PodTemplateSpec, object runtime.Object, controllerRef *metav1.OwnerReference) error {
	if err := validateControllerRef(controllerRef); err != nil {
		return err
	}
	pod, err := GetPodFromTemplate(template, object, controllerRef)
	if err != nil {
		return err
	}
	if len(labels.Set(pod.Labels)) == 0 {
		return fmt.Errorf("failed to create pod, no labels")
	}

	pod.SetNamespace(namespace)
	if err = r.Create(ctx, pod); err != nil {
		if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
			// TODO: 打印个事件
		}
		return err
	}

	return nil
}

func (r *PodSetReconciler) createPodsInBatch(count int, initialBatchSize int, fn func() error) (int, error) {
	errCh := make(chan error, count)
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()

	return 0, nil
}

func (r *PodSetReconciler) calculateStatus(podSet *pixiuv1alpha1.PodSet, filteredPods []*corev1.Pod, replicasErr error) pixiuv1alpha1.PodSetStatus {
	return pixiuv1alpha1.PodSetStatus{}
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueuePod := handler.EnqueueRequestsFromMapFunc(r.mapToPods)

	return ctrl.NewControllerManagedBy(mgr).
		For(&pixiuv1alpha1.PodSet{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, enqueuePod).
		Complete(r)
}
