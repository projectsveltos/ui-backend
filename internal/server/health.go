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
	"sort"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	"github.com/projectsveltos/addon-controller/lib/healthmonitoring"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	libsveltosset "github.com/projectsveltos/libsveltos/lib/set"
)

// healthSummary is what the health join needs from a ClusterSummary, with the cluster it is for.
type healthSummary struct {
	clusterKey string
	input      healthmonitoring.SummaryInput
}

// healthReport is the compact copy of a HealthCheckReport, with the cluster it is for.
type healthReport struct {
	clusterKey string
	input      healthmonitoring.ReportInput
}

// clusterSets maps a cluster key to a set of object references. An entry exists only while its set is not empty.
type clusterSets map[string]*libsveltosset.Set

func (c clusterSets) insert(clusterKey string, ref *corev1.ObjectReference) {
	if c[clusterKey] == nil {
		c[clusterKey] = &libsveltosset.Set{}
	}
	c[clusterKey].Insert(ref)
}

func (c clusterSets) erase(clusterKey string, ref *corev1.ObjectReference) {
	set, ok := c[clusterKey]
	if !ok {
		return
	}
	set.Erase(ref)
	if set.Len() == 0 {
		delete(c, clusterKey)
	}
}

func (c clusterSets) items(clusterKey string) []corev1.ObjectReference {
	set, ok := c[clusterKey]
	if !ok {
		return nil
	}
	items := set.Items()
	sort.Slice(items, func(i, j int) bool {
		if items[i].Namespace != items[j].Namespace {
			return items[i].Namespace < items[j].Namespace
		}
		return items[i].Name < items[j].Name
	})
	return items
}

// healthCache keeps what is needed to answer "how healthy is this cluster". All maps are protected by mux.
// The three controllers (ClusterSummary, HealthCheck, HealthCheckReport) deliver events in any order: each
// updates its own maps and recomputes the one cluster it touches from whatever the maps hold at that moment.
type healthCache struct {
	mux sync.RWMutex

	// summaries: ClusterSummary -> the part of it the join needs
	summaries map[corev1.ObjectReference]healthSummary
	// summariesByCluster: cluster key -> ClusterSummaries of that cluster
	summariesByCluster clusterSets

	// generatedChecks: HealthCheck name -> ClusterSummary it was generated for. A delete event
	// carries only the name, so this is the primary map.
	generatedChecks map[string]corev1.ObjectReference
	// generatedCheckBySummary: ClusterSummary -> name of the HealthCheck generated for it
	generatedCheckBySummary map[corev1.ObjectReference]string

	// reports: HealthCheckReport -> compact copy
	reports map[corev1.ObjectReference]healthReport
	// reportsByCluster: cluster key -> HealthCheckReports of that cluster
	reportsByCluster clusterSets

	// clusterStatus: cluster key -> health state. No entry when no profile of the cluster has monitoring
	// enabled and no report exists.
	clusterStatus map[string]healthmonitoring.State
}

func newHealthCache() *healthCache {
	return &healthCache{
		summaries:               make(map[corev1.ObjectReference]healthSummary),
		summariesByCluster:      make(clusterSets),
		generatedChecks:         make(map[string]corev1.ObjectReference),
		generatedCheckBySummary: make(map[corev1.ObjectReference]string),
		reports:                 make(map[corev1.ObjectReference]healthReport),
		reportsByCluster:        make(clusterSets),
		clusterStatus:           make(map[string]healthmonitoring.State),
	}
}

func (h *healthCache) setSummary(ref *corev1.ObjectReference, clusterKey string,
	input *healthmonitoring.SummaryInput) {

	h.mux.Lock()
	defer h.mux.Unlock()

	previous, found := h.summaries[*ref]
	h.summaries[*ref] = healthSummary{clusterKey: clusterKey, input: *input}
	h.summariesByCluster.insert(clusterKey, ref)
	h.recompute(clusterKey)

	if found && previous.clusterKey != clusterKey {
		h.summariesByCluster.erase(previous.clusterKey, ref)
		h.recompute(previous.clusterKey)
	}
}

// removeSummary leaves the generated check maps alone: they belong to the HealthCheck events.
func (h *healthCache) removeSummary(ref *corev1.ObjectReference) {
	// Called for every ClusterSummary without health monitoring: do not take the write lock for nothing
	h.mux.RLock()
	_, known := h.summaries[*ref]
	h.mux.RUnlock()
	if !known {
		return
	}

	h.mux.Lock()
	defer h.mux.Unlock()

	existing, found := h.summaries[*ref]
	if !found {
		return
	}

	delete(h.summaries, *ref)
	h.summariesByCluster.erase(existing.clusterKey, ref)
	h.recompute(existing.clusterKey)
}

func (h *healthCache) setGeneratedCheck(name string, owner *corev1.ObjectReference) {
	h.mux.Lock()
	defer h.mux.Unlock()

	// The check was generated for another ClusterSummary before: that one loses it
	previousOwner, hadOwner := h.generatedChecks[name]
	if hadOwner && previousOwner != *owner && h.generatedCheckBySummary[previousOwner] == name {
		delete(h.generatedCheckBySummary, previousOwner)
		h.recomputeSummaryCluster(&previousOwner)
	}

	// The ClusterSummary had another check before: that name is not its anymore
	if previousName, ok := h.generatedCheckBySummary[*owner]; ok && previousName != name {
		delete(h.generatedChecks, previousName)
	}

	h.generatedChecks[name] = *owner
	h.generatedCheckBySummary[*owner] = name
	h.recomputeSummaryCluster(owner)
}

func (h *healthCache) removeGeneratedCheck(name string) {
	h.mux.Lock()
	defer h.mux.Unlock()

	owner, found := h.generatedChecks[name]
	if !found {
		return
	}

	delete(h.generatedChecks, name)
	if h.generatedCheckBySummary[owner] == name {
		delete(h.generatedCheckBySummary, owner)
	}
	h.recomputeSummaryCluster(&owner)
}

func (h *healthCache) setReport(ref *corev1.ObjectReference, clusterKey string, input healthmonitoring.ReportInput) {
	h.mux.Lock()
	defer h.mux.Unlock()

	previous, found := h.reports[*ref]
	h.reports[*ref] = healthReport{clusterKey: clusterKey, input: input}
	h.reportsByCluster.insert(clusterKey, ref)
	h.recompute(clusterKey)

	if found && previous.clusterKey != clusterKey {
		h.reportsByCluster.erase(previous.clusterKey, ref)
		h.recompute(previous.clusterKey)
	}
}

func (h *healthCache) removeReport(ref *corev1.ObjectReference) {
	h.mux.Lock()
	defer h.mux.Unlock()

	existing, found := h.reports[*ref]
	if !found {
		return
	}

	delete(h.reports, *ref)
	h.reportsByCluster.erase(existing.clusterKey, ref)
	h.recompute(existing.clusterKey)
}

// state returns the health state of a cluster, and false when nothing monitors it: no profile
// with monitoring enabled and no report.
func (h *healthCache) state(clusterKey string) (healthmonitoring.State, bool) {
	h.mux.RLock()
	defer h.mux.RUnlock()

	state, ok := h.clusterStatus[clusterKey]
	return state, ok
}

// evaluate runs the join for one cluster.
func (h *healthCache) evaluate(clusterKey string) healthmonitoring.ClusterHealth {
	h.mux.RLock()
	defer h.mux.RUnlock()

	return healthmonitoring.Evaluate(h.summaryInputs(clusterKey), h.reportInputs(clusterKey))
}

// recompute refreshes the state of one cluster. Must be called with mux held for writing.
func (h *healthCache) recompute(clusterKey string) {
	state := healthmonitoring.Evaluate(h.summaryInputs(clusterKey), h.reportInputs(clusterKey)).State
	if state == healthmonitoring.StateNotEnabled {
		delete(h.clusterStatus, clusterKey)
		return
	}
	h.clusterStatus[clusterKey] = state
}

// recomputeSummaryCluster refreshes the cluster of a ClusterSummary, if the ClusterSummary is known.
// Must be called with mux held for writing.
func (h *healthCache) recomputeSummaryCluster(summary *corev1.ObjectReference) {
	if known, ok := h.summaries[*summary]; ok {
		h.recompute(known.clusterKey)
	}
}

// summaryInputs returns the inputs of the ClusterSummaries of a cluster, each with the
// HealthCheck generated for it. Must be called with mux held.
func (h *healthCache) summaryInputs(clusterKey string) []healthmonitoring.SummaryInput {
	refs := h.summariesByCluster.items(clusterKey)
	inputs := make([]healthmonitoring.SummaryInput, 0, len(refs))
	for i := range refs {
		input := h.summaries[refs[i]].input
		input.GeneratedCheck = h.generatedCheckBySummary[refs[i]]
		inputs = append(inputs, input)
	}
	return inputs
}

// reportInputs returns the inputs of the reports of a cluster. Must be called with mux held.
func (h *healthCache) reportInputs(clusterKey string) []healthmonitoring.ReportInput {
	refs := h.reportsByCluster.items(clusterKey)
	inputs := make([]healthmonitoring.ReportInput, 0, len(refs))
	for i := range refs {
		inputs = append(inputs, h.reports[refs[i]].input)
	}
	return inputs
}

// AddHealthCheck records the ClusterSummary a HealthCheck was generated for. A HealthCheck
// which is not a generated one (no label/annotations) is forgotten.
func (m *instance) AddHealthCheck(healthCheck metav1.Object) {
	owner, ok := healthmonitoring.GeneratedCheckOwner(healthCheck)
	if !ok {
		m.health.removeGeneratedCheck(healthCheck.GetName())
		return
	}

	m.health.setGeneratedCheck(healthCheck.GetName(), owner)
}

func (m *instance) RemoveHealthCheck(name string) {
	m.health.removeGeneratedCheck(name)
}

func (m *instance) AddHealthCheckReport(report *libsveltosv1beta1.HealthCheckReport) {
	clusterKey := clusterIssuesKey(report.Spec.ClusterType, report.Spec.ClusterNamespace, report.Spec.ClusterName)
	m.health.setReport(healthCheckReportRef(report.Namespace, report.Name), clusterKey,
		healthmonitoring.NewReportInput(report))
}

func (m *instance) RemoveHealthCheckReport(namespace, name string) {
	m.health.removeReport(healthCheckReportRef(namespace, name))
}

// addHealthSummary records what the health join needs from a ClusterSummary.
func (m *instance) addHealthSummary(summary *configv1beta1.ClusterSummary, csRef *corev1.ObjectReference,
	clusterKey string) {

	input, err := healthmonitoring.NewSummaryInput(summary, "")
	if err != nil {
		return
	}

	// Most ClusterSummaries have no health monitoring: keep only the ones with it, and forget
	// the ones that had it and no longer do.
	if !input.Enabled {
		m.health.removeSummary(csRef)
		return
	}

	m.health.setSummary(csRef, clusterKey, &input)
}

// ClusterHealthStatus returns the health state of a cluster. Empty when no profile of the
// cluster has health monitoring enabled and no health report exists for it.
func (m *instance) ClusterHealthStatus(clusterType libsveltosv1beta1.ClusterType,
	clusterNamespace, clusterName string) healthmonitoring.State {

	state, ok := m.health.state(clusterIssuesKey(clusterType, clusterNamespace, clusterName))
	if !ok {
		return ""
	}
	return state
}

// GetClusterHealth returns the health of a cluster: state, per profile state, unhealthy resources.
func (m *instance) GetClusterHealth(clusterType libsveltosv1beta1.ClusterType,
	clusterNamespace, clusterName string) healthmonitoring.ClusterHealth {

	return m.health.evaluate(clusterIssuesKey(clusterType, clusterNamespace, clusterName))
}

func healthCheckReportRef(namespace, name string) *corev1.ObjectReference {
	return &corev1.ObjectReference{
		Namespace:  namespace,
		Name:       name,
		Kind:       libsveltosv1beta1.HealthCheckReportKind,
		APIVersion: libsveltosv1beta1.GroupVersion.String(),
	}
}

// TransformHealthCheckReport is the cache transform for HealthCheckReports. On top of managed fields
// it drops the collected resources (ResourceStatuses[].Resource), the only large field: nothing here reads it.
func TransformHealthCheckReport() toolscache.TransformFunc {
	stripManagedFields := cache.TransformStripManagedFields()

	return func(obj interface{}) (interface{}, error) {
		obj, err := stripManagedFields(obj)
		if err != nil {
			return obj, err
		}

		if report, ok := obj.(*libsveltosv1beta1.HealthCheckReport); ok {
			for i := range report.Spec.ResourceStatuses {
				report.Spec.ResourceStatuses[i].Resource = nil
			}
		}
		return obj, nil
	}
}
