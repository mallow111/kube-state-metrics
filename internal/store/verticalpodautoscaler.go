/*
Copyright 2019 The Kubernetes Authors All rights reserved.

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

package store

import (
	"context"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	autoscaling "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1beta2"
	vpaclientset "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/client/clientset/versioned"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"k8s.io/kube-state-metrics/v2/pkg/constant"
	"k8s.io/kube-state-metrics/v2/pkg/metric"
	generator "k8s.io/kube-state-metrics/v2/pkg/metric_generator"
)

var (
	descVerticalPodAutoscalerAnnotationsName     = "kube_verticalpodautoscaler_annotations"
	descVerticalPodAutoscalerAnnotationsHelp     = "Kubernetes annotations converted to Prometheus labels."
	descVerticalPodAutoscalerLabelsName          = "kube_verticalpodautoscaler_labels"
	descVerticalPodAutoscalerLabelsHelp          = "Kubernetes labels converted to Prometheus labels."
	descVerticalPodAutoscalerLabelsDefaultLabels = []string{"namespace", "verticalpodautoscaler", "target_api_version", "target_kind", "target_name"}
)

func vpaMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator {
	return []generator.FamilyGenerator{
		*generator.NewFamilyGenerator(
			descVerticalPodAutoscalerAnnotationsName,
			descVerticalPodAutoscalerAnnotationsHelp,
			metric.Gauge,
			"",
			wrapVPAFunc(func(a *autoscaling.VerticalPodAutoscaler) *metric.Family {
				annotationKeys, annotationValues := createPrometheusLabelKeysValues("annotation", a.Annotations, allowAnnotationsList)
				return &metric.Family{
					Metrics: []*metric.Metric{
						{
							LabelKeys:   annotationKeys,
							LabelValues: annotationValues,
							Value:       1,
						},
					},
				}
			}),
		),
		*generator.NewFamilyGenerator(
			descVerticalPodAutoscalerLabelsName,
			descVerticalPodAutoscalerLabelsHelp,
			metric.Gauge,
			"",
			wrapVPAFunc(func(a *autoscaling.VerticalPodAutoscaler) *metric.Family {
				labelKeys, labelValues := createPrometheusLabelKeysValues("label", a.Labels, allowLabelsList)
				return &metric.Family{
					Metrics: []*metric.Metric{
						{
							LabelKeys:   labelKeys,
							LabelValues: labelValues,
							Value:       1,
						},
					},
				}
			}),
		),
		*generator.NewFamilyGenerator(
			"kube_verticalpodautoscaler_spec_updatepolicy_updatemode",
			"Update mode of the VerticalPodAutoscaler.",
			metric.Gauge,
			"",
			wrapVPAFunc(func(a *autoscaling.VerticalPodAutoscaler) *metric.Family {
				ms := []*metric.Metric{}

				if a.Spec.UpdatePolicy == nil || a.Spec.UpdatePolicy.UpdateMode == nil {
					return &metric.Family{
						Metrics: ms,
					}
				}

				for _, mode := range []autoscaling.UpdateMode{
					autoscaling.UpdateModeOff,
					autoscaling.UpdateModeInitial,
					autoscaling.UpdateModeRecreate,
					autoscaling.UpdateModeAuto,
				} {
					var v float64
					if *a.Spec.UpdatePolicy.UpdateMode == mode {
						v = 1
					} else {
						v = 0
					}
					ms = append(ms, &metric.Metric{
						LabelKeys:   []string{"update_mode"},
						LabelValues: []string{string(mode)},
						Value:       v,
					})
				}

				return &metric.Family{
					Metrics: ms,
				}
			}),
		),
		*generator.NewFamilyGenerator(
			"kube_verticalpodautoscaler_spec_resourcepolicy_container_policies_minallowed",
			"Minimum resources the VerticalPodAutoscaler can set for containers matching the name.",
			metric.Gauge,
			"",
			wrapVPAFunc(func(a *autoscaling.VerticalPodAutoscaler) *metric.Family {
				ms := []*metric.Metric{}
				if a.Spec.ResourcePolicy == nil || a.Spec.ResourcePolicy.ContainerPolicies == nil {
					return &metric.Family{
						Metrics: ms,
					}
				}

				for _, c := range a.Spec.ResourcePolicy.ContainerPolicies {
					ms = append(ms, vpaResourcesToMetrics(c.ContainerName, c.MinAllowed)...)

				}
				return &metric.Family{
					Metrics: ms,
				}
			}),
		),
		*generator.NewFamilyGenerator(
			"kube_verticalpodautoscaler_spec_resourcepolicy_container_policies_maxallowed",
			"Maximum resources the VerticalPodAutoscaler can set for containers matching the name.",
			metric.Gauge,
			"",
			wrapVPAFunc(func(a *autoscaling.VerticalPodAutoscaler) *metric.Family {
				ms := []*metric.Metric{}
				if a.Spec.ResourcePolicy == nil || a.Spec.ResourcePolicy.ContainerPolicies == nil {
					return &metric.Family{
						Metrics: ms,
					}
				}

				for _, c := range a.Spec.ResourcePolicy.ContainerPolicies {
					ms = append(ms, vpaResourcesToMetrics(c.ContainerName, c.MaxAllowed)...)
				}
				return &metric.Family{
					Metrics: ms,
				}
			}),
		),
		*generator.NewFamilyGenerator(
			"kube_verticalpodautoscaler_status_recommendation_containerrecommendations_lowerbound",
			"Minimum resources the container can use before the VerticalPodAutoscaler updater evicts it.",
			metric.Gauge,
			"",
			wrapVPAFunc(func(a *autoscaling.VerticalPodAutoscaler) *metric.Family {
				ms := []*metric.Metric{}
				if a.Status.Recommendation == nil || a.Status.Recommendation.ContainerRecommendations == nil {
					return &metric.Family{
						Metrics: ms,
					}
				}

				for _, c := range a.Status.Recommendation.ContainerRecommendations {
					ms = append(ms, vpaResourcesToMetrics(c.ContainerName, c.LowerBound)...)
				}
				return &metric.Family{
					Metrics: ms,
				}
			}),
		),
		*generator.NewFamilyGenerator(
			"kube_verticalpodautoscaler_status_recommendation_containerrecommendations_upperbound",
			"Maximum resources the container can use before the VerticalPodAutoscaler updater evicts it.",
			metric.Gauge,
			"",
			wrapVPAFunc(func(a *autoscaling.VerticalPodAutoscaler) *metric.Family {
				ms := []*metric.Metric{}
				if a.Status.Recommendation == nil || a.Status.Recommendation.ContainerRecommendations == nil {
					return &metric.Family{
						Metrics: ms,
					}
				}

				for _, c := range a.Status.Recommendation.ContainerRecommendations {
					ms = append(ms, vpaResourcesToMetrics(c.ContainerName, c.UpperBound)...)
				}
				return &metric.Family{
					Metrics: ms,
				}
			}),
		),
		*generator.NewFamilyGenerator(
			"kube_verticalpodautoscaler_status_recommendation_containerrecommendations_target",
			"Target resources the VerticalPodAutoscaler recommends for the container.",
			metric.Gauge,
			"",
			wrapVPAFunc(func(a *autoscaling.VerticalPodAutoscaler) *metric.Family {
				ms := []*metric.Metric{}
				if a.Status.Recommendation == nil || a.Status.Recommendation.ContainerRecommendations == nil {
					return &metric.Family{
						Metrics: ms,
					}
				}
				for _, c := range a.Status.Recommendation.ContainerRecommendations {
					ms = append(ms, vpaResourcesToMetrics(c.ContainerName, c.Target)...)
				}
				return &metric.Family{
					Metrics: ms,
				}
			}),
		),
		*generator.NewFamilyGenerator(
			"kube_verticalpodautoscaler_status_recommendation_containerrecommendations_uncappedtarget",
			"Target resources the VerticalPodAutoscaler recommends for the container ignoring bounds.",
			metric.Gauge,
			"",
			wrapVPAFunc(func(a *autoscaling.VerticalPodAutoscaler) *metric.Family {
				ms := []*metric.Metric{}
				if a.Status.Recommendation == nil || a.Status.Recommendation.ContainerRecommendations == nil {
					return &metric.Family{
						Metrics: ms,
					}
				}
				for _, c := range a.Status.Recommendation.ContainerRecommendations {
					ms = append(ms, vpaResourcesToMetrics(c.ContainerName, c.UncappedTarget)...)
				}
				return &metric.Family{
					Metrics: ms,
				}
			}),
		),
	}
}

func vpaResourcesToMetrics(containerName string, resources v1.ResourceList) []*metric.Metric {
	ms := []*metric.Metric{}
	for resourceName, val := range resources {
		switch resourceName {
		case v1.ResourceCPU:
			ms = append(ms, &metric.Metric{
				LabelValues: []string{containerName, sanitizeLabelName(string(resourceName)), string(constant.UnitCore)},
				Value:       float64(val.MilliValue()) / 1000,
			})
		case v1.ResourceStorage:
			fallthrough
		case v1.ResourceEphemeralStorage:
			fallthrough
		case v1.ResourceMemory:
			ms = append(ms, &metric.Metric{
				LabelValues: []string{containerName, sanitizeLabelName(string(resourceName)), string(constant.UnitByte)},
				Value:       float64(val.Value()),
			})
		}
	}
	for _, metric := range ms {
		metric.LabelKeys = []string{"container", "resource", "unit"}
	}
	return ms
}

func wrapVPAFunc(f func(*autoscaling.VerticalPodAutoscaler) *metric.Family) func(interface{}) *metric.Family {
	return func(obj interface{}) *metric.Family {
		vpa := obj.(*autoscaling.VerticalPodAutoscaler)

		metricFamily := f(vpa)
		targetRef := vpa.Spec.TargetRef

		// targetRef was not a mandatory field, which can lead to a nil pointer exception here.
		// However, we still want to expose metrics to be able:
		// * to alert about VPA objects without target refs
		// * to count the right amount of VPA objects in a cluster
		if targetRef == nil {
			targetRef = &autoscalingv1.CrossVersionObjectReference{}
		}

		for _, m := range metricFamily.Metrics {
			m.LabelKeys = append(descVerticalPodAutoscalerLabelsDefaultLabels, m.LabelKeys...)
			m.LabelValues = append([]string{vpa.Namespace, vpa.Name, targetRef.APIVersion, targetRef.Kind, targetRef.Name}, m.LabelValues...)
		}

		return metricFamily
	}
}

func createVPAListWatchFunc(vpaClient vpaclientset.Interface) func(kubeClient clientset.Interface, ns string) cache.ListerWatcher {
	return func(kubeClient clientset.Interface, ns string) cache.ListerWatcher {
		return &cache.ListWatch{
			ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
				return vpaClient.AutoscalingV1beta2().VerticalPodAutoscalers(ns).List(context.TODO(), opts)
			},
			WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
				return vpaClient.AutoscalingV1beta2().VerticalPodAutoscalers(ns).Watch(context.TODO(), opts)
			},
		}
	}
}
