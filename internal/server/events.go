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
	"sort"
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

// Return a list of existing EventTriggers. If canListAll is false, the result is
// filtered down to only the EventTriggers user has explicit get access to.
func (m *instance) getEventTriggers(ctx context.Context, canListAll bool, user string, groups []string,
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

		if !canListAll {
			ok, err := m.canGetEventTrigger(et.Name, user, groups)
			if err != nil || !ok {
				continue
			}
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
	// MatchingResourcesCount is the number of resources matching the EventSource in the cluster.
	// The resources are served, paginated, by /eventResources.
	MatchingResourcesCount int `json:"matchingResourcesCount"`
}

type EventTriggerDetails struct {
	EventTriggerName    string                         `json:"eventTriggerName"`
	ClusterSelector     libsveltosv1beta1.Selector     `json:"clusterSelector"`
	EventSource         *libsveltosv1beta1.EventSource `json:"eventSource,omitempty"`
	ClusterEventMatches []ClusterEventMatch            `json:"clusterEventMatches,omitempty"`
}

// Returns a detailed view of an EventTrigger. This includes:
// - Referenced EventSource YAML
// - List of matching clusters (including the number of matching resources in each cluster)
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

	eventReport, err := getEventReport(ctx, c, eventSourceName, clusterproxy.GetClusterType(cluster),
		cluster.Namespace, cluster.Name, logger)
	if err != nil {
		return nil, err
	}
	if eventReport != nil {
		result.MatchingResourcesCount = len(eventReport.Spec.MatchingResources)
	}

	return result, nil
}

// getEventReport returns the EventReport of an EventSource for a cluster, nil if it does not exist.
func getEventReport(ctx context.Context, c client.Client, eventSourceName string,
	clusterType libsveltosv1beta1.ClusterType, clusterNamespace, clusterName string,
	logger logr.Logger) (*libsveltosv1beta1.EventReport, error) {

	eventReportName := libsveltosv1beta1.GetEventReportName(eventSourceName, clusterName, &clusterType)

	eventReport := &libsveltosv1beta1.EventReport{}
	err := c.Get(ctx,
		types.NamespacedName{Namespace: clusterNamespace, Name: eventReportName},
		eventReport)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		logger.V(logs.LogInfo).Error(err, "failed to get eventReport")
		return nil, err
	}

	return eventReport, nil
}

// EventResourcesResult is the response of /eventResources.
type EventResourcesResult struct {
	// TotalResources is the number of resources matching the filters, before pagination.
	TotalResources int        `json:"totalResources"`
	Resources      []Resource `json:"resources"`
}

// getEventMatchingResources returns the resources matching the EventSource of an EventTrigger in a cluster:
// filtered (a case insensitive substring match on kind, namespace and name), ordered by group, kind,
// namespace and name, then paginated. Returns nil when the EventTrigger or its EventSource does not exist.
func getEventMatchingResources(ctx context.Context, c client.Client, eventTriggerName string,
	clusterType libsveltosv1beta1.ClusterType, clusterNamespace, clusterName string,
	filters *resourceFilters, limit, skip int, logger logr.Logger) (*EventResourcesResult, error) {

	eventTrigger := &eventv1beta1.EventTrigger{}
	err := c.Get(ctx, types.NamespacedName{Name: eventTriggerName}, eventTrigger)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		logger.V(logs.LogInfo).Error(err, "failed to get eventTrigger")
		return nil, err
	}

	eventReport, err := getEventReport(ctx, c, eventTrigger.Spec.EventSourceName, clusterType,
		clusterNamespace, clusterName, logger)
	if err != nil {
		return nil, err
	}

	var resources []Resource
	if eventReport != nil {
		resources = newEventResources(eventReport.Spec.MatchingResources)
	}
	resources = filterEventResources(resources, filters)
	sortEventResources(resources)

	page, err := getSliceInRange(resources, limit, skip)
	if err != nil {
		return nil, err
	}
	if page == nil {
		page = []Resource{}
	}

	return &EventResourcesResult{TotalResources: len(resources), Resources: page}, nil
}

func newEventResources(references []corev1.ObjectReference) []Resource {
	resources := make([]Resource, len(references))
	for i := range references {
		gvk := schema.FromAPIVersionAndKind(references[i].APIVersion, references[i].Kind)

		resources[i].Namespace = references[i].Namespace
		resources[i].Name = references[i].Name
		resources[i].Kind = references[i].Kind
		resources[i].Group = gvk.Group
	}

	return resources
}

func filterEventResources(resources []Resource, filters *resourceFilters) []Resource {
	if filters == nil || (filters.Kind == "" && filters.Namespace == "" && filters.Name == "") {
		return resources
	}

	filtered := make([]Resource, 0, len(resources))
	for i := range resources {
		if containsFold(resources[i].Kind, filters.Kind) &&
			containsFold(resources[i].Namespace, filters.Namespace) &&
			containsFold(resources[i].Name, filters.Name) {

			filtered = append(filtered, resources[i])
		}
	}

	return filtered
}

func sortEventResources(resources []Resource) {
	sort.SliceStable(resources, func(i, j int) bool {
		a, b := &resources[i], &resources[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Namespace != b.Namespace {
			return a.Namespace < b.Namespace
		}
		return a.Name < b.Name
	})
}
