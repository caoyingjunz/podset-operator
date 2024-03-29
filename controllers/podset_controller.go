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
	"reflect"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	pixiuv1alpha1 "github.com/caoyingjunz/podset-operator/api/v1alpha1"
	"github.com/caoyingjunz/podset-operator/pkg/metrics"
	pixiutypes "github.com/caoyingjunz/podset-operator/pkg/types"
)

const (
	// The number of times we retry updating a PosSet's status.
	statusUpdateRetries = 1
)

const (
	FailedCreatePodReason     = "FailedCreate"
	SuccessfulCreatePodReason = "SuccessfulCreate"
)

// PodSetReconciler reconciles a PodSet object
type PodSetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger

	Recorder        record.EventRecorder
	MetricsProvider metrics.MetricsProvider
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
	log.Info("reconciling pod set operator")

	// HandleMetrics
	defer r.MetricsProvider.HandleMetrics()

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

	updatePS, err := r.updatePodSetStatus(podSet, newStatus)
	if err != nil {
		// TODO: Resync the PodSet after MinReadySeconds
		// MinReadySeconds will be supported
		return reconcile.Result{RequeueAfter: time.Duration(0) * time.Second}, nil
	}

	if replicasErr == nil &&
		updatePS.Status.ReadyReplicas == *(updatePS.Spec.Replicas) &&
		updatePS.Status.AvailableReplicas != *(updatePS.Spec.Replicas) {
		return reconcile.Result{RequeueAfter: time.Duration(0) * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *PodSetReconciler) manageReplicas(ctx context.Context, filteredPods []*corev1.Pod, podSet *pixiuv1alpha1.PodSet) error {
	diff := len(filteredPods) - int(*podSet.Spec.Replicas)
	if diff < 0 {
		diff *= -1
		if diff > pixiutypes.BurstReplicas {
			diff = pixiutypes.BurstReplicas
		}
		r.Log.Info("Too few replicas", "podSet", klog.KObj(podSet), "need", *(podSet.Spec.Replicas), "creating", diff)
		_, err := r.createPodsInBatch(diff, 1, func() error {
			if err := r.createPod(ctx, podSet.Namespace, &podSet.Spec.Template, podSet, metav1.NewControllerRef(podSet, pixiuv1alpha1.GroupVersionKind)); err != nil {
				return err
			}
			return nil
		})

		return err

	} else if diff > 0 {
		if diff > pixiutypes.BurstReplicas {
			diff = pixiutypes.BurstReplicas
		}
		r.Log.Info("Too many replicas", "podSet", klog.KObj(podSet), "need", *(podSet.Spec.Replicas), "deleting", diff)
		podToDelete := getPodsToDelete(filteredPods, diff)

		errCh := make(chan error, diff)
		var wg sync.WaitGroup
		wg.Add(diff)
		for _, pod := range podToDelete {
			go func(targetPod *corev1.Pod) {
				defer wg.Done()
				if err := r.deletePod(ctx, targetPod.Namespace, targetPod.Name); err != nil {
					if !apierrors.IsNotFound(err) {
						errCh <- err
					}
				}
			}(pod)
		}
		wg.Wait()

		select {
		case err := <-errCh:
			if err != nil {
				return err
			}
		default:
		}
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
		// return fmt.Errorf("failed to create pod, no labels")
		// TODO: CRD 在存储 spec.template 为空
		ps := object.(*pixiuv1alpha1.PodSet)
		pod.Labels = ps.Spec.Selector.MatchLabels
	}

	pod.SetNamespace(namespace)
	if err = r.Create(ctx, pod); err != nil {
		if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
			r.Recorder.Eventf(object, corev1.EventTypeWarning, FailedCreatePodReason, "Error creating: %v", err)
		}
		return err
	}

	r.Recorder.Eventf(object, corev1.EventTypeNormal, SuccessfulCreatePodReason, "Created pod: %v", pod.Name)
	return nil
}

func (r *PodSetReconciler) deletePod(ctx context.Context, namespace string, name string) error {
	pod := &corev1.Pod{}
	pod.SetNamespace(namespace)
	pod.SetName(name)
	if err := r.Delete(ctx, pod); err != nil {
		if apierrors.IsNotFound(err) {
			klog.V(4).Infof("pod %v/%v has already been deleted.", namespace, name)
			return err
		}

		return fmt.Errorf("failed to delete pod: %v", err)
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

func (r *PodSetReconciler) calculateStatus(podSet *pixiuv1alpha1.PodSet, filteredPods []*corev1.Pod, podSetErr error) pixiuv1alpha1.PodSetStatus {
	newStatus := podSet.Status

	readyReplicasCount := 0
	availableReplicasCount := 0
	for _, pod := range filteredPods {
		// TODO: 通过 label match pods
		if IsPodReady(pod) {
			readyReplicasCount++
			if IsPodAvailable(pod, 0, metav1.Now()) {
				availableReplicasCount++
			}
		}
	}

	failureCond := GetCondition(podSet.Status, pixiutypes.PodSetFailure)
	if podSetErr != nil && failureCond == nil {
		var reason string
		if diff := len(filteredPods) - int(*podSet.Spec.Replicas); diff < 0 {
			reason = "FailedCreate"
		} else if diff > 0 {
			reason = "FailedDelete"
		}
		cond := NewReplicaSetCondition(pixiutypes.PodSetFailure, corev1.ConditionTrue, reason, podSetErr.Error())
		SetCondition(&newStatus, cond)
	} else if podSetErr == nil && failureCond != nil {
		RemoveCondition(&newStatus, pixiutypes.PodSetFailure)
	}

	// TODO: the default availableReplicas is 1
	if availableReplicasCount >= 1 {
		SetCondition(&newStatus, NewReplicaSetCondition(pixiutypes.PodSetSuccess, corev1.ConditionTrue, pixiutypes.MinimumReplicasAvailable, "PodSet has minimum availability."))
	} else {
		SetCondition(&newStatus, NewReplicaSetCondition(pixiutypes.PodSetSuccess, corev1.ConditionTrue, pixiutypes.MinimumReplicasUnavailable, "PodSet does not have minimum availability."))
	}

	newStatus.Replicas = int32(len(filteredPods))
	newStatus.ReadyReplicas = int32(readyReplicasCount)
	newStatus.AvailableReplicas = int32(availableReplicasCount)
	return newStatus
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodSetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueuePod := handler.EnqueueRequestsFromMapFunc(r.mapToPods)

	return ctrl.NewControllerManagedBy(mgr).
		For(&pixiuv1alpha1.PodSet{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, enqueuePod).
		Complete(r)
}

// updateReplicaSetStatus attempts to update the Status.Replicas of the given ReplicaSet, with a single GET/PUT retry.
func (r *PodSetReconciler) updatePodSetStatus(ps *pixiuv1alpha1.PodSet, newStatus pixiuv1alpha1.PodSetStatus) (*pixiuv1alpha1.PodSet, error) {
	if ps.Status.Replicas == newStatus.Replicas &&
		ps.Status.ReadyReplicas == newStatus.ReadyReplicas &&
		ps.Status.AvailableReplicas == newStatus.AvailableReplicas &&
		ps.Generation == newStatus.ObservedGeneration &&
		reflect.DeepEqual(ps.Status.Conditions, newStatus.Conditions) {
		return ps, nil
	}

	// Save the generation number we acted on, otherwise we might wrongfully indicate
	// that we've seen a spec update when we retry.
	// TODO: This can clobber an update if we allow multiple agents to write to the
	// same status.
	newStatus.ObservedGeneration = ps.Generation

	for i, ps := 0, ps; ; i++ {
		klog.Infof(fmt.Sprintf("Updating status for %v: %s/%s, ", ps.Kind, ps.Namespace, ps.Name) +
			fmt.Sprintf("replicas %d->%d (need %d), ", ps.Status.Replicas, newStatus.Replicas, *(ps.Spec.Replicas)) +
			fmt.Sprintf("readyReplicas %d->%d, ", ps.Status.ReadyReplicas, newStatus.ReadyReplicas) +
			fmt.Sprintf("availableReplicas %d->%d, ", ps.Status.AvailableReplicas, newStatus.AvailableReplicas))

		ps.Status = newStatus
		if err := r.Status().Update(context.TODO(), ps); err != nil {
			return nil, err
		}

		// Stop retrying if we exceed statusUpdateRetries - the podSet will be requeued.
		if i >= statusUpdateRetries {
			break
		}

		// Get the PodSet with the latest resource version for the next poll
		if err := r.Get(context.TODO(), types.NamespacedName{Namespace: ps.Namespace, Name: ps.Name}, ps); err != nil {
			return nil, err
		}
	}

	return ps, nil
}

func getPodsToDelete(filteredPods []*corev1.Pod, diff int) []*corev1.Pod {
	return filteredPods[:diff]
}
