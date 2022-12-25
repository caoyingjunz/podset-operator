package metrics

import (
	"k8s.io/component-base/metrics"
)

const PodsetControllerSubsystem = "poset_controller"

var SortingDeletionAgeRatio = metrics.NewHistogram(
	&metrics.HistogramOpts{
		Subsystem: PodsetControllerSubsystem,
		Name:      "sorting_deletion_age_ratio",
		Help: "The ratio of chosen deleted pod's ages to the current youngest pod's age (at the time). Should be <2." +
			"The intent of this metric is to measure the rough efficacy of the LogarithmicScaleDown feature gate's effect on" +
			"the sorting (and deletion) of pods when a replicaset scales down. This only considers Ready pods when calculating and reporting.",
		Buckets:        metrics.ExponentialBuckets(0.25, 2, 6),
		StabilityLevel: metrics.ALPHA,
	},
)

// Register registers ReplicaSet controller metrics.
func Register(registrationFunc func(metrics.Registerable) error) error {
	return registrationFunc(SortingDeletionAgeRatio)
}
