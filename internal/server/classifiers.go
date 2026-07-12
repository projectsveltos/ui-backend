/*
Copyright 2026. projectsveltos.io. All rights reserved.

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
	"fmt"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
)

type ClassifierSummary struct {
	Name string `json:"name"`
	// Type is either Classifier or ManagementClusterClassifier
	Type                 string `json:"type"`
	LabelCount           int    `json:"labelCount"`
	MatchingClusterCount int    `json:"matchingClusterCount"`
}

type ClassifierSummaries []ClassifierSummary

func (s ClassifierSummaries) Len() int      { return len(s) }
func (s ClassifierSummaries) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s ClassifierSummaries) Less(i, j int) bool {
	if s[i].Name != s[j].Name {
		return s[i].Name < s[j].Name
	}
	return s[i].Type < s[j].Type
}

type ClassifiersResult struct {
	TotalClassifiers int                 `json:"totalClassifiers"`
	Classifiers      []ClassifierSummary `json:"classifiers"`
}

type ClassifierLabelEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type UnManagedLabelEntry struct {
	Key            string `json:"key"`
	FailureMessage string `json:"failureMessage,omitempty"`
}

type MatchingClusterEntry struct {
	ClusterNamespace string                 `json:"clusterNamespace"`
	ClusterName      string                 `json:"clusterName"`
	ClusterType      string                 `json:"clusterType"`
	ManagedLabels    []ClassifierLabelEntry `json:"managedLabels,omitempty"`
	ConflictedLabels []UnManagedLabelEntry  `json:"conflictedLabels,omitempty"`
}

type ClassifierDetails struct {
	Name string `json:"name"`
	// Type is either Classifier or ManagementClusterClassifier
	Type string `json:"type"`

	// Classifier-only
	ResourceSelectors            []libsveltosv1beta1.ResourceSelector            `json:"resourceSelectors,omitempty"`
	AggregatedClassification     string                                          `json:"aggregatedClassification,omitempty"`
	KubernetesVersionConstraints []libsveltosv1beta1.KubernetesVersionConstraint `json:"kubernetesVersionConstraints,omitempty"`

	// ManagementClusterClassifier-only
	MatchResources    []libsveltosv1beta1.ResourceSelector `json:"matchResources,omitempty"`
	ClassificationLua string                               `json:"classificationLua,omitempty"`

	// shared
	ClassifierLabels []ClassifierLabelEntry `json:"classifierLabels"`
	MatchingClusters []MatchingClusterEntry `json:"matchingClusters"`
}

type classifierFilters struct {
	Name             string `uri:"name"`
	ClusterNamespace string `uri:"clusterNamespace"`
	ClusterName      string `uri:"clusterName"`
}

func getClassifierFiltersFromQuery(c *gin.Context) *classifierFilters {
	var filters classifierFilters
	filters.Name = c.Query("name")
	filters.ClusterNamespace = c.Query("cluster_namespace")
	filters.ClusterName = c.Query("cluster_name")

	return &filters
}

func clusterFilterRequested(filters *classifierFilters) bool {
	return filters.ClusterNamespace != "" || filters.ClusterName != ""
}

func classifierNameMatches(name string, filters *classifierFilters) bool {
	return filters.Name == "" ||
		strings.Contains(strings.ToLower(name), strings.ToLower(filters.Name))
}

func getClassifiersInRange(classifiers []ClassifierSummary, limit, skip int) ([]ClassifierSummary, error) {
	return getSliceInRange(classifiers, limit, skip)
}

// getClassifiers returns a summary of all Classifier and ManagementClusterClassifier
// instances the user can access. If filters identifies a cluster, the result is
// narrowed to only instances currently matching that cluster; each entry's
// MatchingClusterCount still reflects the instance's total match count, not just
// the filtered cluster.
func (m *instance) getClassifiers(ctx context.Context, canListClassifiers, canListManagementClusterClassifiers bool,
	user string, filters *classifierFilters) (ClassifierSummaries, error) {

	reportsByClassifier, reportsByMCC, err := m.listClassifierReportsByName(ctx)
	if err != nil {
		return nil, err
	}

	result := ClassifierSummaries{}

	classifiers := &libsveltosv1beta1.ClassifierList{}
	if err := m.client.List(ctx, classifiers); err != nil {
		return nil, err
	}

	for i := range classifiers.Items {
		classifier := &classifiers.Items[i]
		if !classifier.GetDeletionTimestamp().IsZero() {
			continue
		}

		if !canListClassifiers {
			ok, err := m.canGetClassifier(classifier.Name, user)
			if err != nil || !ok {
				continue
			}
		}

		if !classifierNameMatches(classifier.Name, filters) {
			continue
		}

		reports := reportsByClassifier[classifier.Name]
		if clusterFilterRequested(filters) && !classifierReportsMatchCluster(reports, filters) {
			continue
		}

		result = append(result, ClassifierSummary{
			Name:                 classifier.Name,
			Type:                 libsveltosv1beta1.ClassifierKind,
			LabelCount:           len(classifier.Spec.ClassifierLabels),
			MatchingClusterCount: countMatchingClassifierReports(reports),
		})
	}

	mccs := &libsveltosv1beta1.ManagementClusterClassifierList{}
	if err := m.client.List(ctx, mccs); err != nil {
		return nil, err
	}

	for i := range mccs.Items {
		mcc := &mccs.Items[i]
		if !mcc.GetDeletionTimestamp().IsZero() {
			continue
		}

		if !canListManagementClusterClassifiers {
			ok, err := m.canGetManagementClusterClassifier(mcc.Name, user)
			if err != nil || !ok {
				continue
			}
		}

		if !classifierNameMatches(mcc.Name, filters) {
			continue
		}

		reports := reportsByMCC[mcc.Name]
		if clusterFilterRequested(filters) && !managementClusterClassifierReportsMatchCluster(reports, filters) {
			continue
		}

		result = append(result, ClassifierSummary{
			Name:                 mcc.Name,
			Type:                 libsveltosv1beta1.ManagementClusterClassifierKind,
			LabelCount:           len(mcc.Spec.ClassifierLabels),
			MatchingClusterCount: countMatchingManagementClusterClassifierReports(reports),
		})
	}

	return result, nil
}

// listClassifierReportsByName lists all ClassifierReport and ManagementClusterClassifierReport
// instances once and groups them by the owning classifier/managementClusterClassifier name.
func (m *instance) listClassifierReportsByName(ctx context.Context,
) (byClassifier map[string][]libsveltosv1beta1.ClassifierReport,
	byMCC map[string][]libsveltosv1beta1.ManagementClusterClassifierReport, err error) {

	classifierReports := &libsveltosv1beta1.ClassifierReportList{}
	if err := m.client.List(ctx, classifierReports); err != nil {
		return nil, nil, err
	}

	reportsByClassifier := map[string][]libsveltosv1beta1.ClassifierReport{}
	for i := range classifierReports.Items {
		name := classifierReports.Items[i].Spec.ClassifierName
		reportsByClassifier[name] = append(reportsByClassifier[name], classifierReports.Items[i])
	}

	mccReports := &libsveltosv1beta1.ManagementClusterClassifierReportList{}
	if err := m.client.List(ctx, mccReports); err != nil {
		return nil, nil, err
	}

	reportsByMCC := map[string][]libsveltosv1beta1.ManagementClusterClassifierReport{}
	for i := range mccReports.Items {
		name := mccReports.Items[i].Spec.ClassifierName
		reportsByMCC[name] = append(reportsByMCC[name], mccReports.Items[i])
	}

	return reportsByClassifier, reportsByMCC, nil
}

// clusterMatchesFilters reports whether a cluster matches filters. Each filter field is
// only applied when non-empty, using case-insensitive substring matching (same convention
// as EventTrigger's cluster filters) so a user can filter on just namespace or just name
// without having to fully specify the cluster.
func clusterMatchesFilters(clusterNamespace, clusterName string, filters *classifierFilters) bool {
	if filters.ClusterNamespace != "" &&
		!strings.Contains(strings.ToLower(clusterNamespace), strings.ToLower(filters.ClusterNamespace)) {

		return false
	}
	if filters.ClusterName != "" &&
		!strings.Contains(strings.ToLower(clusterName), strings.ToLower(filters.ClusterName)) {

		return false
	}
	return true
}

// classifierReportsMatchCluster returns true if any report indicates the Classifier
// currently matches a cluster satisfying filters. Reports with an empty
// ClusterName (not yet associated with a cluster) are ignored.
func classifierReportsMatchCluster(reports []libsveltosv1beta1.ClassifierReport, filters *classifierFilters) bool {
	for i := range reports {
		r := &reports[i]
		if r.Spec.ClusterName == "" || !r.Spec.Match {
			continue
		}
		if clusterMatchesFilters(r.Spec.ClusterNamespace, r.Spec.ClusterName, filters) {
			return true
		}
	}
	return false
}

// managementClusterClassifierReportsMatchCluster returns true if any report indicates
// the ManagementClusterClassifier currently targets a cluster satisfying filters.
// Unlike ClassifierReport, there is no Match field: a report only exists for clusters
// the Lua evaluation currently returns.
func managementClusterClassifierReportsMatchCluster(reports []libsveltosv1beta1.ManagementClusterClassifierReport,
	filters *classifierFilters) bool {

	for i := range reports {
		r := &reports[i]
		if r.Spec.ClusterName == "" {
			continue
		}
		if clusterMatchesFilters(r.Spec.ClusterNamespace, r.Spec.ClusterName, filters) {
			return true
		}
	}
	return false
}

func countMatchingClassifierReports(reports []libsveltosv1beta1.ClassifierReport) int {
	count := 0
	for i := range reports {
		if reports[i].Spec.ClusterName != "" && reports[i].Spec.Match {
			count++
		}
	}
	return count
}

func countMatchingManagementClusterClassifierReports(reports []libsveltosv1beta1.ManagementClusterClassifierReport) int {
	count := 0
	for i := range reports {
		if reports[i].Spec.ClusterName != "" {
			count++
		}
	}
	return count
}

// GetClassifierDetails returns the spec, configured labels, and matching clusters
// (with the labels actually owned on each) for a Classifier or ManagementClusterClassifier
// instance. Returns nil, nil if the instance is not found.
func (m *instance) GetClassifierDetails(ctx context.Context, name, classifierType string,
) (*ClassifierDetails, error) {

	switch classifierType {
	case libsveltosv1beta1.ClassifierKind:
		return m.getClassifierDetails(ctx, name)
	case libsveltosv1beta1.ManagementClusterClassifierKind:
		return m.getManagementClusterClassifierDetails(ctx, name)
	default:
		return nil, fmt.Errorf("unsupported classifier type %q", classifierType)
	}
}

func (m *instance) getClassifierDetails(ctx context.Context, name string) (*ClassifierDetails, error) {
	classifier := &libsveltosv1beta1.Classifier{}
	err := m.client.Get(ctx, types.NamespacedName{Name: name}, classifier)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	reports := &libsveltosv1beta1.ClassifierReportList{}
	if err := m.client.List(ctx, reports); err != nil {
		return nil, err
	}

	ownReports := make([]libsveltosv1beta1.ClassifierReport, 0)
	for i := range reports.Items {
		if reports.Items[i].Spec.ClassifierName == name {
			ownReports = append(ownReports, reports.Items[i])
		}
	}

	result := &ClassifierDetails{
		Name:                         name,
		Type:                         libsveltosv1beta1.ClassifierKind,
		KubernetesVersionConstraints: classifier.Spec.KubernetesVersionConstraints,
		ClassifierLabels:             toClassifierLabelEntries(classifier.Spec.ClassifierLabels),
		MatchingClusters:             matchingClustersFromClassifierReports(ownReports, classifier.Spec.ClassifierLabels),
	}
	if classifier.Spec.DeployedResourceConstraint != nil {
		result.ResourceSelectors = classifier.Spec.DeployedResourceConstraint.ResourceSelectors
		result.AggregatedClassification = classifier.Spec.DeployedResourceConstraint.AggregatedClassification
	}

	return result, nil
}

func (m *instance) getManagementClusterClassifierDetails(ctx context.Context, name string,
) (*ClassifierDetails, error) {

	mcc := &libsveltosv1beta1.ManagementClusterClassifier{}
	err := m.client.Get(ctx, types.NamespacedName{Name: name}, mcc)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	reports := &libsveltosv1beta1.ManagementClusterClassifierReportList{}
	if err := m.client.List(ctx, reports); err != nil {
		return nil, err
	}

	ownReports := make([]libsveltosv1beta1.ManagementClusterClassifierReport, 0)
	for i := range reports.Items {
		if reports.Items[i].Spec.ClassifierName == name {
			ownReports = append(ownReports, reports.Items[i])
		}
	}

	return &ClassifierDetails{
		Name:              name,
		Type:              libsveltosv1beta1.ManagementClusterClassifierKind,
		MatchResources:    mcc.Spec.MatchResources,
		ClassificationLua: mcc.Spec.ClassificationLua,
		ClassifierLabels:  toClassifierLabelEntries(mcc.Spec.ClassifierLabels),
		MatchingClusters:  matchingClustersFromManagementClusterClassifierReports(ownReports, mcc.Spec.ClassifierLabels),
	}, nil
}

func toClassifierLabelEntries(labels []libsveltosv1beta1.ClassifierLabel) []ClassifierLabelEntry {
	entries := make([]ClassifierLabelEntry, len(labels))
	for i := range labels {
		entries[i] = ClassifierLabelEntry{Key: labels[i].Key, Value: labels[i].Value}
	}
	return entries
}

func classifierLabelValues(labels []libsveltosv1beta1.ClassifierLabel) map[string]string {
	values := make(map[string]string, len(labels))
	for i := range labels {
		values[labels[i].Key] = labels[i].Value
	}
	return values
}

func toManagedLabelEntries(keys []string, values map[string]string) []ClassifierLabelEntry {
	if len(keys) == 0 {
		return nil
	}
	entries := make([]ClassifierLabelEntry, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, ClassifierLabelEntry{Key: key, Value: values[key]})
	}
	return entries
}

func toUnManagedLabelEntries(unmanaged []libsveltosv1beta1.UnManagedLabel) []UnManagedLabelEntry {
	if len(unmanaged) == 0 {
		return nil
	}
	entries := make([]UnManagedLabelEntry, 0, len(unmanaged))
	for i := range unmanaged {
		entry := UnManagedLabelEntry{Key: unmanaged[i].Key}
		if unmanaged[i].FailureMessage != nil {
			entry.FailureMessage = *unmanaged[i].FailureMessage
		}
		entries = append(entries, entry)
	}
	return entries
}

func sortMatchingClusters(entries []MatchingClusterEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ClusterNamespace != entries[j].ClusterNamespace {
			return entries[i].ClusterNamespace < entries[j].ClusterNamespace
		}
		return entries[i].ClusterName < entries[j].ClusterName
	})
}

func matchingClustersFromClassifierReports(reports []libsveltosv1beta1.ClassifierReport,
	classifierLabels []libsveltosv1beta1.ClassifierLabel) []MatchingClusterEntry {

	values := classifierLabelValues(classifierLabels)

	result := make([]MatchingClusterEntry, 0, len(reports))
	for i := range reports {
		r := &reports[i]
		if r.Spec.ClusterName == "" || !r.Spec.Match {
			continue
		}
		result = append(result, MatchingClusterEntry{
			ClusterNamespace: r.Spec.ClusterNamespace,
			ClusterName:      r.Spec.ClusterName,
			ClusterType:      string(r.Spec.ClusterType),
			ManagedLabels:    toManagedLabelEntries(r.Status.ManagedLabels, values),
			ConflictedLabels: toUnManagedLabelEntries(r.Status.UnManagedLabels),
		})
	}

	sortMatchingClusters(result)
	return result
}

func matchingClustersFromManagementClusterClassifierReports(reports []libsveltosv1beta1.ManagementClusterClassifierReport,
	classifierLabels []libsveltosv1beta1.ClassifierLabel) []MatchingClusterEntry {

	values := classifierLabelValues(classifierLabels)

	result := make([]MatchingClusterEntry, 0, len(reports))
	for i := range reports {
		r := &reports[i]
		if r.Spec.ClusterName == "" {
			continue
		}
		result = append(result, MatchingClusterEntry{
			ClusterNamespace: r.Spec.ClusterNamespace,
			ClusterName:      r.Spec.ClusterName,
			ClusterType:      string(r.Spec.ClusterType),
			ManagedLabels:    toManagedLabelEntries(r.Status.ManagedLabels, values),
			ConflictedLabels: toUnManagedLabelEntries(r.Status.UnManagedLabels),
		})
	}

	sortMatchingClusters(result)
	return result
}
