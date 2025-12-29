/*
Copyright 2025. projectsveltos.io. All rights reserved.

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

package server

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	eventv1beta1 "github.com/projectsveltos/event-manager/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/libsveltos/lib/clusterproxy"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
)

type EventTrigger struct {
	// Name of the profile
	Name string `json:"name"`
	// Number of Matching clusters
	MatchingClusters int `json:"matchingClusters"`
}

type EventTriggers []EventTrigger

type EventResult struct {
	TotalEvents   int            `json:"totalEvents"`
	EventTriggers []EventTrigger `json:"eventTriggers"`
}

func (s EventTriggers) Len() int      { return len(s) }
func (s EventTriggers) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s EventTriggers) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

type eventFilters struct {
	Name             string `uri:"name"`
	ClusterName      string `uri:"clusterName"`
	ClusterNamespace string `uri:"clusterNamespace"`
}

func getEventFiltersFromQuery(c *gin.Context) *eventFilters {
	var filters eventFilters
	// Get the values from query parameters
	filters.Name = c.Query("name")
	filters.ClusterNamespace = c.Query("cluster_namespace")
	filters.ClusterName = c.Query("cluster_name")

	return &filters
}

// getEventData returns a map of all eventTriggers.
func getEventData(eventTriggers map[corev1.ObjectReference]EventTriggerInfo, filters *eventFilters,
) EventTriggers {

	result := EventTriggers{}

	for k := range eventTriggers {
		eventTrigger := eventTriggers[k]

		if filters.Name != "" {
			if !strings.Contains(
				strings.ToLower(k.Name),
				strings.ToLower(filters.Name)) {

				continue
			}
		}

		if !isEventMatchingClusterFilters(eventTriggers[k], filters) {
			continue
		}

		result = append(result, EventTrigger{
			Name:             k.Name,
			MatchingClusters: len(eventTrigger.MatchingClusters),
		})
	}

	return result
}

func isEventMatchingClusterFilters(eventTriggerInfo EventTriggerInfo, filters *eventFilters) bool {
	if filters.ClusterNamespace == "" &&
		filters.ClusterName == "" {

		return true
	}

	for i := range eventTriggerInfo.MatchingClusters {
		if filters.ClusterNamespace != "" {
			if !strings.Contains(
				strings.ToLower(eventTriggerInfo.MatchingClusters[i].Namespace),
				strings.ToLower(filters.ClusterNamespace)) {

				continue
			}
		}

		if filters.ClusterName != "" {
			if !strings.Contains(
				strings.ToLower(eventTriggerInfo.MatchingClusters[i].Name),
				strings.ToLower(filters.ClusterName)) {

				continue
			}
		}

		return true
	}

	return false
}

func getEventsInRange(eventTriggers []EventTrigger, limit, skip int) ([]EventTrigger, error) {
	return getSliceInRange(eventTriggers, limit, skip)
}

// Return a list of existing EventTriggers
func (m *instance) getEventTriggers(ctx context.Context,
) (map[corev1.ObjectReference]EventTriggerInfo, error) {

	eventTriggers := &eventv1beta1.EventTriggerList{}
	err := m.client.List(ctx, eventTriggers)
	if err != nil {
		return nil, err
	}

	result := map[corev1.ObjectReference]EventTriggerInfo{}
	for i := range eventTriggers.Items {
		et := &eventTriggers.Items[i]

		if !et.GetDeletionTimestamp().IsZero() {
			continue
		}

		eventRef := corev1.ObjectReference{
			Kind:       eventv1beta1.EventTriggerKind,
			APIVersion: eventv1beta1.GroupVersion.String(),
			Name:       et.Name,
		}

		eventInfo := EventTriggerInfo{
			MatchingClusters: et.Status.MatchingClusterRefs,
			ClusterSelector:  et.Spec.SourceClusterSelector,
		}

		result[eventRef] = eventInfo
	}

	return result, nil
}

type ClusterEventMatch struct {
	ClusterNamespace string `json:"clusterNamespace"`
	ClusterName      string `json:"clusterName"`
	ClusterKind      string `json:"clusterKind"`
	ClusterInfo
	Resources []Resource `json:"resources,omitempty"`
}

type EventTriggerDetails struct {
	EventTriggerName    string                         `json:"eventTriggerName"`
	ClusterSelector     libsveltosv1beta1.Selector     `json:"clusterSelector"`
	EventSource         *libsveltosv1beta1.EventSource `json:"eventSource,omitempty"`
	ClusterEventMatches []ClusterEventMatch            `json:"clusterEventMatches,omitempty"`
}

// Returns a detailed view of an EventTrigger. This includes:
// - Referenced EventSource YAML
// - List of matching clusters (including matching resources in each cluster)
func (m *instance) GetEventTriggerDetails(ctx context.Context, eventTriggerName string,
	capiClusters, sveltosClusters map[corev1.ObjectReference]ClusterInfo, logger logr.Logger,
) (*EventTriggerDetails, error) {

	result := &EventTriggerDetails{}

	eventTrigger := &eventv1beta1.EventTrigger{}
	err := m.client.Get(ctx, types.NamespacedName{Name: eventTriggerName}, eventTrigger)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		logger.V(logs.LogInfo).Error(err, "failed to get eventTrigger")
		return nil, err
	}
	result.EventTriggerName = eventTriggerName
	result.ClusterSelector = eventTrigger.Spec.SourceClusterSelector

	eventSource := &libsveltosv1beta1.EventSource{}
	err = m.client.Get(ctx, types.NamespacedName{Name: eventTrigger.Spec.EventSourceName},
		eventSource)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		logger.V(logs.LogInfo).Error(err, "failed to get eventSource")
		return nil, err
	}
	result.EventSource = &libsveltosv1beta1.EventSource{
		TypeMeta: eventSource.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name: eventSource.Name,
		},
		Spec: eventSource.Spec,
	}

	result.ClusterEventMatches = make([]ClusterEventMatch, 0, len(eventTrigger.Status.MatchingClusterRefs))
	for i := range eventTrigger.Status.MatchingClusterRefs {
		cluster := &eventTrigger.Status.MatchingClusterRefs[i]
		clusterDetails, err := getEventClusterDetails(ctx, m.client,
			eventTrigger.Spec.EventSourceName, cluster, capiClusters, sveltosClusters, logger)
		if err != nil {
			return nil, err
		}
		if clusterDetails == nil {
			continue
		}
		result.ClusterEventMatches = append(result.ClusterEventMatches, *clusterDetails)
	}

	return result, nil
}

func getEventClusterDetails(ctx context.Context, c client.Client, eventSourceName string,
	cluster *corev1.ObjectReference, capiClusters, sveltosClusters map[corev1.ObjectReference]ClusterInfo,
	logger logr.Logger) (*ClusterEventMatch, error) {

	var clusterInfo ClusterInfo
	var ok bool
	if cluster.Kind == clusterv1.ClusterKind {
		clusterInfo, ok = capiClusters[*cluster]
	} else {
		clusterInfo, ok = sveltosClusters[*cluster]
	}

	if !ok {
		return nil, nil
	}

	result := &ClusterEventMatch{
		ClusterNamespace: cluster.Namespace,
		ClusterName:      cluster.Name,
		ClusterKind:      cluster.Kind,
		ClusterInfo:      clusterInfo,
	}

	clusterType := clusterproxy.GetClusterType(cluster)
	eventReportName := libsveltosv1beta1.GetEventReportName(eventSourceName, cluster.Name, &clusterType)

	eventReport := &libsveltosv1beta1.EventReport{}
	err := c.Get(ctx,
		types.NamespacedName{Namespace: cluster.Namespace, Name: eventReportName},
		eventReport)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return result, nil
		}
		logger.V(logs.LogInfo).Error(err, "failed to get eventReport")
		return nil, err
	}

	result.Resources = make([]Resource, len(eventReport.Spec.MatchingResources))
	for i := range eventReport.Spec.MatchingResources {
		gvk := schema.FromAPIVersionAndKind(eventReport.Spec.MatchingResources[i].APIVersion,
			eventReport.Spec.MatchingResources[i].Kind)

		result.Resources[i].Namespace = eventReport.Spec.MatchingResources[i].Namespace
		result.Resources[i].Name = eventReport.Spec.MatchingResources[i].Name
		result.Resources[i].Kind = eventReport.Spec.MatchingResources[i].Kind
		result.Resources[i].Group = gvk.Group
	}

	return result, nil
}
