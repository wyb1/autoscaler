/*
Copyright 2018 The Kubernetes Authors.

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

// Package recommender (aka metrics_recommender) - code for metrics of VPA Recommender
package recommender

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/resource"
	vpa_types "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta1"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/utils/metrics"
)

const (
	metricsNamespace = metrics.TopMetricsNamespace + "recommender"
)

var (
	modes = []string{string(vpa_types.UpdateModeOff), string(vpa_types.UpdateModeInitial), string(vpa_types.UpdateModeRecreate), string(vpa_types.UpdateModeAuto)}
)

var (
	vpaContainerRecommendations = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "vpa_recommendation",
			Help:      "Recommendation from the VPA",
		}, []string{"vpa_name", "namespace", "pod_selector", "container", "recommendation_type", "resource_name"},
	)

	vpaObjectCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "vpa_objects_count",
			Help:      "Number of VPA objects present in the cluster.",
		}, []string{"update_mode", "has_recommendation"},
	)

	recommendationLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Name:      "recommendation_latency_seconds",
			Help:      "Time elapsed from creating a valid VPA configuration to the first recommendation.",
			Buckets:   []float64{1.0, 2.0, 5.0, 7.5, 10.0, 20.0, 30.0, 40.00, 50.0, 60.0, 90.0, 120.0, 150.0, 180.0, 240.0, 300.0, 600.0, 900.0, 1800.0},
		},
	)

	functionLatency = metrics.CreateExecutionTimeMetric(metricsNamespace,
		"Time spent in various parts of VPA Recommender main loop.")
)

type objectCounterKey struct {
	mode string
	has  bool
}

// ObjectCounter helps split all VPA objects into buckets
type ObjectCounter struct {
	cnt map[objectCounterKey]int
}

// Register initializes all metrics for VPA Recommender
func Register() {
	prometheus.MustRegister(vpaContainerRecommendations)
	prometheus.MustRegister(vpaObjectCount)
	prometheus.MustRegister(recommendationLatency)
	prometheus.MustRegister(functionLatency)
}

// NewExecutionTimer provides a timer for Recommender's RunOnce execution
func NewExecutionTimer() *metrics.ExecutionTimer {
	return metrics.NewExecutionTimer(functionLatency)
}

// ObserveRecommendationLatency observes the time it took for the first recommendation to appear
func ObserveRecommendationLatency(created time.Time) {
	recommendationLatency.Observe(time.Now().Sub(created).Seconds())
}

func toFloat64(q *resource.Quantity) float64 {
	v := q.Value()
	if v > 10000000 {
		return float64(v)
	}
	return float64(q.MilliValue()) * 0.001
}

func ObserveVPARecommendation(vpa *model.Vpa) {
	for _, r := range vpa.Recommendation.ContainerRecommendations {
		vpaContainerRecommendations.WithLabelValues(vpa.ID.VpaName, vpa.ID.Namespace, vpa.PodSelector.String(), r.ContainerName, "lower_bound", "cpu").Set(toFloat64(r.LowerBound.Cpu()))
		vpaContainerRecommendations.WithLabelValues(vpa.ID.VpaName, vpa.ID.Namespace, vpa.PodSelector.String(), r.ContainerName, "lower_bound", "memory").Set(toFloat64(r.LowerBound.Memory()))
		vpaContainerRecommendations.WithLabelValues(vpa.ID.VpaName, vpa.ID.Namespace, vpa.PodSelector.String(), r.ContainerName, "target", "cpu").Set(toFloat64(r.Target.Cpu()))
		vpaContainerRecommendations.WithLabelValues(vpa.ID.VpaName, vpa.ID.Namespace, vpa.PodSelector.String(), r.ContainerName, "target", "memory").Set(toFloat64(r.Target.Memory()))
		vpaContainerRecommendations.WithLabelValues(vpa.ID.VpaName, vpa.ID.Namespace, vpa.PodSelector.String(), r.ContainerName, "upper_bound", "cpu").Set(toFloat64(r.UpperBound.Cpu()))
		vpaContainerRecommendations.WithLabelValues(vpa.ID.VpaName, vpa.ID.Namespace, vpa.PodSelector.String(), r.ContainerName, "upper_bound", "memory").Set(toFloat64(r.UpperBound.Memory()))
	}
}

// NewObjectCounter creates a new helper to split VPA objects into buckets
func NewObjectCounter() *ObjectCounter {
	obj := ObjectCounter{
		cnt: make(map[objectCounterKey]int),
	}

	// initialize with empty data so we can clean stale gauge values in Observe
	for _, m := range modes {
		obj.cnt[objectCounterKey{mode: m, has: false}] = 0
		obj.cnt[objectCounterKey{mode: m, has: true}] = 0
	}

	return &obj
}

// Add updates the helper state to include the given VPA object
func (oc *ObjectCounter) Add(vpa *model.Vpa) {
	var mode string
	if vpa.UpdateMode != nil {
		mode = string(*vpa.UpdateMode)
	}
	key := objectCounterKey{
		mode: mode,
		has:  vpa.HasRecommendation(),
	}
	oc.cnt[key]++
}

// Observe passes all the computed bucket values to metrics
func (oc *ObjectCounter) Observe() {
	for k, v := range oc.cnt {
		vpaObjectCount.WithLabelValues(k.mode, fmt.Sprintf("%v", k.has)).Set(float64(v))
	}
}
