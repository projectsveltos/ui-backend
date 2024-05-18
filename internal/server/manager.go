/*
Copyright 2024. projectsveltos.io. All rights reserved.

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
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"

	configv1alpha1 "github.com/projectsveltos/addon-controller/api/v1alpha1"
	libsveltosv1alpha1 "github.com/projectsveltos/libsveltos/api/v1alpha1"
)

type ClusterInfo struct {
	Labels         map[string]string `json:"labels"`
	Version        string            `json:"version"`
	Ready          bool              `json:"ready"`
	FailureMessage *string           `json:"failureMessage"`
}

type ClusterProfileStatus struct {
	Name        *string                        `json:"name"`
	Namespace   *string                        `json:"namespace"`
	ClusterType libsveltosv1alpha1.ClusterType `json:"clusterType"`
	ClusterName *string                        `json:"clusterName"`
	Summary     []ClusterFeatureSummary        `json:"summary"`
}

type ClusterFeatureSummary struct {
	FeatureID      configv1alpha1.FeatureID     `json:"featureID"`
	Status         configv1alpha1.FeatureStatus `json:"status,omitempty"`
	FailureMessage *string                      `json:"failureMessage,omitempty"`
}

type instance struct {
	client             client.Client
	scheme             *runtime.Scheme
	clusterMux         sync.RWMutex // use a Mutex to update managed Clusters
	clusterStatusesMux sync.RWMutex // mutex to update cached ClusterSummary instances

	sveltosClusters      map[corev1.ObjectReference]ClusterInfo
	capiClusters         map[corev1.ObjectReference]ClusterInfo
	clusterSummaryReport map[corev1.ObjectReference]ClusterProfileStatus
}

var (
	managerInstance            *instance
	lock                       = &sync.RWMutex{}
	failingClusterSummaryTypes = []configv1alpha1.FeatureStatus{
		configv1alpha1.FeatureStatusFailed,
		configv1alpha1.FeatureStatusFailedNonRetriable,
		configv1alpha1.FeatureStatusProvisioning,
	}
)

// InitializeManagerInstance initializes manager instance
func InitializeManagerInstance(ctx context.Context, c client.Client, scheme *runtime.Scheme,
	port string, logger logr.Logger) {

	if managerInstance == nil {
		lock.Lock()
		defer lock.Unlock()
		if managerInstance == nil {
			managerInstance = &instance{
				client:               c,
				sveltosClusters:      make(map[corev1.ObjectReference]ClusterInfo),
				capiClusters:         make(map[corev1.ObjectReference]ClusterInfo),
				clusterSummaryReport: make(map[corev1.ObjectReference]ClusterProfileStatus),
				clusterMux:           sync.RWMutex{},
				clusterStatusesMux:   sync.RWMutex{},
				scheme:               scheme,
			}

			go func() {
				managerInstance.start(ctx, port, logger)
			}()
		}
	}
}

func GetManagerInstance() *instance {
	return managerInstance
}

func (m *instance) GetManagedSveltosClusters() map[corev1.ObjectReference]ClusterInfo {
	m.clusterMux.RLock()
	defer m.clusterMux.RUnlock()
	return m.sveltosClusters
}

func (m *instance) GetManagedCAPIClusters() map[corev1.ObjectReference]ClusterInfo {
	m.clusterMux.RLock()
	defer m.clusterMux.RUnlock()
	return m.capiClusters
}

func (m *instance) GetClusterProfileStatuses() map[corev1.ObjectReference]ClusterProfileStatus {
	m.clusterStatusesMux.RLock()
	defer m.clusterStatusesMux.RUnlock()
	return m.clusterSummaryReport
}

func (m *instance) GetClusterProfileStatusesByCluster(
	clusterNamespace,
	clusterName *string,
	clusterType libsveltosv1alpha1.ClusterType) []ClusterProfileStatus {
	m.clusterStatusesMux.Lock()
	defer m.clusterStatusesMux.Unlock()

	clusterProfileStatuses := make([]ClusterProfileStatus, 0)
	for _, clusterProfileStatus := range m.clusterSummaryReport {
		// since we're sure it is a proper cluster summary => we're sure it has this label
		if *clusterProfileStatus.Namespace == *clusterNamespace && *clusterProfileStatus.ClusterName == *clusterName {
			if clusterProfileStatus.ClusterType == clusterType {
				clusterProfileStatuses = append(clusterProfileStatuses, clusterProfileStatus)
			}
		}
	}

	return clusterProfileStatuses
}

func (m *instance) AddSveltosCluster(sveltosCluster *libsveltosv1alpha1.SveltosCluster) {
	info := ClusterInfo{
		Labels:         sveltosCluster.Labels,
		Version:        sveltosCluster.Status.Version,
		Ready:          sveltosCluster.Status.Ready,
		FailureMessage: sveltosCluster.Status.FailureMessage,
	}

	sveltosClusterInfo := getKeyFromObject(m.scheme, sveltosCluster)

	m.clusterMux.Lock()
	defer m.clusterMux.Unlock()

	delete(m.sveltosClusters, *sveltosClusterInfo)
	m.sveltosClusters[*sveltosClusterInfo] = info
}

func (m *instance) RemoveSveltosCluster(sveltosClusterNamespace, sveltosClusterName string) {
	sveltosClusterInfo := &corev1.ObjectReference{
		Namespace:  sveltosClusterNamespace,
		Name:       sveltosClusterName,
		Kind:       libsveltosv1alpha1.SveltosClusterKind,
		APIVersion: libsveltosv1alpha1.GroupVersion.String(),
	}
	m.clusterMux.Lock()
	defer m.clusterMux.Unlock()

	delete(m.sveltosClusters, *sveltosClusterInfo)
}

func (m *instance) AddCAPICluster(cluster *clusterv1.Cluster) {
	info := ClusterInfo{
		Labels:         cluster.Labels,
		Ready:          cluster.Status.ControlPlaneReady,
		FailureMessage: cluster.Status.FailureMessage,
	}
	if cluster.Spec.Topology != nil {
		info.Version = cluster.Spec.Topology.Version
	}

	clusterInfo := getKeyFromObject(m.scheme, cluster)

	m.clusterMux.Lock()
	defer m.clusterMux.Unlock()

	delete(m.capiClusters, *clusterInfo)
	m.capiClusters[*clusterInfo] = info
}

func (m *instance) RemoveCAPICluster(clusterNamespace, clusterName string) {
	clusterInfo := &corev1.ObjectReference{
		Namespace:  clusterNamespace,
		Name:       clusterName,
		Kind:       clusterv1.ClusterKind,
		APIVersion: clusterv1.GroupVersion.String(),
	}
	m.clusterMux.Lock()
	defer m.clusterMux.Unlock()

	delete(m.capiClusters, *clusterInfo)
}

func (m *instance) AddClusterProfileStatus(summary *configv1alpha1.ClusterSummary) {
	if !verifyLabelConfiguration(summary) {
		return
	}

	// we're sure we're adding a proper cluster summary
	// get the cluster profile name by using labels
	profileOwnerRef, err := configv1alpha1.GetProfileOwnerReference(summary)
	if err != nil {
		return
	}

	// initialize feature summaries slice
	clusterFeatureSummaries := MapToClusterFeatureSummaries(&summary.Status.FeatureSummaries)

	clusterProfileStatus := ClusterProfileStatus{
		Name:        &profileOwnerRef.Name,
		Namespace:   &summary.Namespace,
		ClusterType: summary.Spec.ClusterType,
		ClusterName: &summary.Spec.ClusterName,
		Summary:     clusterFeatureSummaries,
	}

	m.clusterStatusesMux.Lock()
	defer m.clusterStatusesMux.Unlock()

	m.clusterSummaryReport[*getKeyFromObject(m.scheme, summary)] = clusterProfileStatus
}

func (m *instance) RemoveClusterProfileStatus(summaryNamespace, summaryName string) {
	clusterProfileStatus := &corev1.ObjectReference{
		Namespace:  summaryNamespace,
		Name:       summaryName,
		Kind:       configv1alpha1.ClusterSummaryKind,
		APIVersion: configv1alpha1.GroupVersion.String(),
	}
	m.clusterStatusesMux.Lock()
	defer m.clusterStatusesMux.Unlock()

	delete(m.clusterSummaryReport, *clusterProfileStatus)
}

// getKeyFromObject returns the Key that can be used in the internal reconciler maps.
func getKeyFromObject(scheme *runtime.Scheme, obj client.Object) *corev1.ObjectReference {
	addTypeInformationToObject(scheme, obj)

	apiVersion, kind := obj.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()

	return &corev1.ObjectReference{
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
		Kind:       kind,
		APIVersion: apiVersion,
	}
}

func addTypeInformationToObject(scheme *runtime.Scheme, obj client.Object) {
	gvks, _, err := scheme.ObjectKinds(obj)
	if err != nil {
		panic(1)
	}

	for _, gvk := range gvks {
		if gvk.Kind == "" {
			continue
		}
		if gvk.Version == "" || gvk.Version == runtime.APIVersionInternal {
			continue
		}
		obj.GetObjectKind().SetGroupVersionKind(gvk)
		break
	}
}

func verifyLabelConfiguration(summary *configv1alpha1.ClusterSummary) bool {

	if summary.Labels == nil {
		return false
	}

	_, err := configv1alpha1.GetProfileOwnerReference(summary)
	if err != nil {
		return false
	}

	return summary.Labels[configv1alpha1.ClusterNameLabel] != "" &&
		summary.Labels[configv1alpha1.ClusterTypeLabel] != ""
}

func MapToClusterFeatureSummaries(featureSummaries *[]configv1alpha1.FeatureSummary) []ClusterFeatureSummary {
	clusterFeatureSummaries := make([]ClusterFeatureSummary, 0, len(*featureSummaries))
	for _, featureSummary := range *featureSummaries {
		clusterFeatureSummary := ClusterFeatureSummary{
			FeatureID:      featureSummary.FeatureID,
			Status:         featureSummary.Status,
			FailureMessage: featureSummary.FailureMessage,
		}
		clusterFeatureSummaries = append(clusterFeatureSummaries, clusterFeatureSummary)
	}

	return clusterFeatureSummaries
}
