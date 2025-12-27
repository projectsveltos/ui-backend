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
	"fmt"
	"sync"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
	libsveltosset "github.com/projectsveltos/libsveltos/lib/set"
)

type ClusterInfo struct {
	Labels         map[string]string `json:"labels"`
	Version        string            `json:"version"`
	Ready          bool              `json:"ready"`
	Paused         bool              `json:"paused"`
	FailureMessage *string           `json:"failureMessage"`
}

type ClusterProfileStatus struct {
	ProfileName string                        `json:"profileName"`
	ProfileType string                        `json:"profileType"`
	Namespace   string                        `json:"namespace"`
	ClusterType libsveltosv1beta1.ClusterType `json:"clusterType"`
	ClusterName string                        `json:"clusterName"`
	Summary     []ClusterFeatureSummary       `json:"summary"`
}

type ClusterFeatureSummary struct {
	FeatureID      libsveltosv1beta1.FeatureID     `json:"featureID"`
	Status         libsveltosv1beta1.FeatureStatus `json:"status,omitempty"`
	FailureMessage *string                         `json:"failureMessage,omitempty"`
}

type ProfileInfo struct {
	// Tier is the ClusterProfile/Profile tier
	Tier int32 `json:"tier"`

	ClusterSelector libsveltosv1beta1.Selector `json:"clusterSelector"`

	// Dependencies is the list of ClusterProfile/Profile dependency's names
	Dependencies *libsveltosset.Set `json:"dependencies"`

	// Dependents is the list of ClusterProfile/Profile dependent's names
	Dependents *libsveltosset.Set `json:"dependents"`
}

type EventTriggerInfo struct {
	ClusterSelector libsveltosv1beta1.Selector `json:"clusterSelector"`

	MatchingClusters []corev1.ObjectReference `json:"matchingClusters"`
}

type instance struct {
	config             *rest.Config
	client             client.Client
	scheme             *runtime.Scheme
	clusterMux         sync.RWMutex // use a Mutex to update managed Clusters
	profileMux         sync.RWMutex // use a Mutex to update cached Profiles
	clusterStatusesMux sync.RWMutex // mutex to update cached ClusterSummary instances
	logger             logr.Logger

	sveltosClusters      map[corev1.ObjectReference]ClusterInfo
	capiClusters         map[corev1.ObjectReference]ClusterInfo
	clusterSummaryReport map[corev1.ObjectReference]ClusterProfileStatus
	profiles             map[corev1.ObjectReference]ProfileInfo
}

var (
	managerInstance *instance
	lock            = &sync.RWMutex{}
)

// InitializeManagerInstance initializes manager instance
func InitializeManagerInstance(ctx context.Context, config *rest.Config, c client.Client,
	scheme *runtime.Scheme, port string, logger logr.Logger) {

	if managerInstance == nil {
		lock.Lock()
		defer lock.Unlock()
		if managerInstance == nil {
			managerInstance = &instance{
				config:               config,
				client:               c,
				sveltosClusters:      make(map[corev1.ObjectReference]ClusterInfo),
				capiClusters:         make(map[corev1.ObjectReference]ClusterInfo),
				clusterSummaryReport: make(map[corev1.ObjectReference]ClusterProfileStatus),
				profiles:             make(map[corev1.ObjectReference]ProfileInfo),
				clusterMux:           sync.RWMutex{},
				clusterStatusesMux:   sync.RWMutex{},
				profileMux:           sync.RWMutex{},
				scheme:               scheme,
				logger:               logger,
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

func (m *instance) GetManagedSveltosClusters(ctx context.Context, canListAll bool, user string,
) (map[corev1.ObjectReference]ClusterInfo, error) {

	// If user can list all SveltosClusters, return cached data
	if canListAll {
		m.clusterMux.RLock()
		defer m.clusterMux.RUnlock()
		return m.sveltosClusters, nil
	}

	// If user cannot list all SveltosClusters, run a List so to get only SveltosClusters user has access to
	// List (vs using m.sveltosClusters) is intentionally done to avoid taking lock for too long
	sveltosClusters := &libsveltosv1beta1.SveltosClusterList{}
	err := m.client.List(ctx, sveltosClusters)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to list SveltosClusters: %v", err))
		return nil, err
	}

	result := map[corev1.ObjectReference]ClusterInfo{}
	for i := range sveltosClusters.Items {
		sc := &sveltosClusters.Items[i]
		ok, err := m.canGetSveltosCluster(sc.Namespace, sc.Name, user)
		if err != nil {
			continue
		}
		if ok {
			info := ClusterInfo{
				Labels:         sc.Labels,
				Version:        sc.Status.Version,
				Ready:          sc.Status.Ready,
				FailureMessage: sc.Status.FailureMessage,
			}

			sveltosClusterInfo := getKeyFromObject(m.scheme, sc)
			result[*sveltosClusterInfo] = info
		}
	}

	return result, nil
}

func (m *instance) GetManagedCAPIClusters(ctx context.Context, canListAll bool, user string,
) (map[corev1.ObjectReference]ClusterInfo, error) {

	// If user can list all CAPI Clusters, return cached data
	if canListAll {
		m.clusterMux.RLock()
		defer m.clusterMux.RUnlock()
		return m.capiClusters, nil
	}

	// If user cannot list all SveltosClusters, run a List so to get only SveltosClusters user has access to
	// List (vs using m.capiClusters) is intentionally done to avoid taking lock for too long
	clusters := &clusterv1.ClusterList{}
	err := m.client.List(ctx, clusters)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to list Clusters: %v", err))
		return nil, err
	}

	result := map[corev1.ObjectReference]ClusterInfo{}
	for i := range clusters.Items {
		capiCluster := &clusters.Items[i]
		ok, err := m.canGetCAPICluster(capiCluster.Namespace, capiCluster.Name, user)
		if err != nil {
			continue
		}
		if ok {
			info := ClusterInfo{
				Labels:         capiCluster.Labels,
				Ready:          derefBoolPtr(capiCluster.Status.Initialization.ControlPlaneInitialized),
				FailureMessage: examineClusterConditions(capiCluster),
			}

			capiClusterInfo := getKeyFromObject(m.scheme, capiCluster)
			result[*capiClusterInfo] = info
		}
	}

	return result, nil
}

func (m *instance) GetClusterProfileStatuses() map[corev1.ObjectReference]ClusterProfileStatus {
	m.clusterStatusesMux.RLock()
	defer m.clusterStatusesMux.RUnlock()
	return m.clusterSummaryReport
}

func (m *instance) GetClusterProfileStatusesByCluster(
	clusterNamespace,
	clusterName *string,
	clusterType libsveltosv1beta1.ClusterType) []ClusterProfileStatus {

	m.clusterStatusesMux.Lock()
	defer m.clusterStatusesMux.Unlock()

	clusterProfileStatuses := make([]ClusterProfileStatus, 0)
	for _, clusterProfileStatus := range m.clusterSummaryReport {
		// since we're sure it is a proper cluster summary => we're sure it has this label
		if clusterProfileStatus.Namespace == *clusterNamespace && clusterProfileStatus.ClusterName == *clusterName &&
			clusterProfileStatus.ClusterType == clusterType {

			clusterProfileStatuses = append(clusterProfileStatuses, clusterProfileStatus)
		}
	}

	return clusterProfileStatuses
}

func (m *instance) AddSveltosCluster(sveltosCluster *libsveltosv1beta1.SveltosCluster) {
	info := ClusterInfo{
		Labels:         sveltosCluster.Labels,
		Version:        sveltosCluster.Status.Version,
		Ready:          sveltosCluster.Status.Ready,
		Paused:         sveltosCluster.Spec.Paused,
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
		Kind:       libsveltosv1beta1.SveltosClusterKind,
		APIVersion: libsveltosv1beta1.GroupVersion.String(),
	}
	m.clusterMux.Lock()
	defer m.clusterMux.Unlock()

	delete(m.sveltosClusters, *sveltosClusterInfo)
}

func derefBoolPtr(ptr *bool) bool {
	if ptr == nil {
		return false
	}

	return *ptr
}

func (m *instance) AddCAPICluster(cluster *clusterv1.Cluster) {
	info := ClusterInfo{
		Labels:         cluster.Labels,
		Ready:          derefBoolPtr(cluster.Status.Initialization.ControlPlaneInitialized),
		Paused:         derefBoolPtr(cluster.Spec.Paused),
		FailureMessage: examineClusterConditions(cluster),
	}

	info.Version = cluster.Spec.Topology.Version

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

func (m *instance) AddClusterProfileStatus(summary *configv1beta1.ClusterSummary) {
	if !verifyLabelConfiguration(summary) {
		return
	}

	// we're sure we're adding a proper cluster summary
	// get the cluster profile name by using labels
	profileOwnerRef, err := configv1beta1.GetProfileOwnerReference(summary)
	if err != nil {
		return
	}

	// initialize feature summaries slice
	clusterFeatureSummaries := MapToClusterFeatureSummaries(&summary.Status.FeatureSummaries)

	clusterProfileStatus := ClusterProfileStatus{
		ProfileName: profileOwnerRef.Name,
		ProfileType: profileOwnerRef.Kind,
		Namespace:   summary.Namespace,
		ClusterType: summary.Spec.ClusterType,
		ClusterName: summary.Spec.ClusterName,
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
		Kind:       configv1beta1.ClusterSummaryKind,
		APIVersion: configv1beta1.GroupVersion.String(),
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

func verifyLabelConfiguration(summary *configv1beta1.ClusterSummary) bool {
	if summary.Labels == nil {
		return false
	}

	_, err := configv1beta1.GetProfileOwnerReference(summary)
	if err != nil {
		return false
	}

	return summary.Labels[configv1beta1.ClusterNameLabel] != "" &&
		summary.Labels[configv1beta1.ClusterTypeLabel] != ""
}

func MapToClusterFeatureSummaries(featureSummaries *[]configv1beta1.FeatureSummary) []ClusterFeatureSummary {
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

func (m *instance) validateToken(token string) error {
	config, err := m.getKubernetesRestConfig(token)
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}

	_, err = clientset.Discovery().ServerVersion()
	if err != nil {
		return err
	}

	return nil
}

func (m *instance) GetProfiles(ctx context.Context, canListClusterProfiles, canListProfiles bool, user string,
) (map[corev1.ObjectReference]ProfileInfo, error) {

	if canListClusterProfiles && canListProfiles {
		m.profileMux.RLock()
		defer m.profileMux.RUnlock()
		return m.profiles, nil
	}

	// To avoid blocking on lock, get a copy of cached profiles and from this point on use only the copied version
	profileCopy := m.getCopyOfProfiles()
	result := map[corev1.ObjectReference]ProfileInfo{}

	err := m.getAccessibleClusterProfiles(ctx, user, profileCopy, result)
	if err != nil {
		return result, err
	}
	err = m.getAccessibleProfiles(ctx, user, profileCopy, result)
	if err != nil {
		return result, err
	}

	return result, nil
}

func (m *instance) AddProfile(profile *corev1.ObjectReference, selector libsveltosv1beta1.Selector,
	tier int32, dependencies *libsveltosset.Set) {

	if dependencies == nil {
		dependencies = &libsveltosset.Set{}
	}

	m.profileMux.Lock()
	defer m.profileMux.Unlock()

	m.appendProfileAsDependent(profile, dependencies)

	profileInfo := m.getProfileInfo(profile)
	// Get cached dependencies
	if profileInfo.Dependencies != nil {
		// Calculate the difference between the previous and current dependency sets for this profileCalculate the difference
		// between the previous and current dependency sets for this profile
		oldDependencies := profileInfo.Dependencies.Difference(dependencies)
		// Remove profile as dependent for obsolete dependencies
		for i := range oldDependencies {
			m.removeProfileDependency(&oldDependencies[i], profile)
		}
	}

	m.profiles[*profile] = ProfileInfo{
		Tier:            tier,
		ClusterSelector: selector,
		Dependencies:    dependencies,
		Dependents:      profileInfo.Dependents,
	}
}

func (m *instance) RemoveProfile(profile *corev1.ObjectReference) {
	m.profileMux.Lock()
	defer m.profileMux.Unlock()

	// Get cached dependencies
	profileInfo := m.profiles[*profile]
	if profileInfo.Dependencies != nil {
		oldDependencies := profileInfo.Dependencies.Items()
		// Remove profile as dependent for obsolete dependencies
		for i := range oldDependencies {
			m.removeProfileDependency(&oldDependencies[i], profile)
		}
	}

	delete(m.profiles, *profile)
}

func (m *instance) GetProfile(profile *corev1.ObjectReference) ProfileInfo {
	m.profileMux.Lock()
	defer m.profileMux.Unlock()

	return m.profiles[*profile]
}

// removeProfileDependency removes oldDependency from source's cached dependents
func (m *instance) removeProfileDependency(source, oldDependency *corev1.ObjectReference) {
	profileInfo, ok := m.profiles[*source]
	if !ok {
		return
	}

	dependents := profileInfo.Dependents
	dependents.Erase(oldDependency)
	profileInfo.Dependents = dependents
	m.profiles[*source] = profileInfo
}

// add profile as dependent for any profile in dependencies.
// Dependencies are profile's dependencies. So profile is a dependent for each one of them.
func (m *instance) appendProfileAsDependent(profile *corev1.ObjectReference, dependencies *libsveltosset.Set) {
	items := dependencies.Items()
	for i := range items {
		p := &corev1.ObjectReference{
			Kind:       profile.Kind,
			APIVersion: profile.APIVersion,
			Namespace:  profile.Namespace,
			Name:       items[i].Name,
		}

		profileInfo := m.getProfileInfo(p)
		profileInfo.Dependents.Insert(profile)
		m.setProfileInfo(p, profileInfo)
	}
}

func (m *instance) getProfileInfo(profile *corev1.ObjectReference) *ProfileInfo {
	profileInfo, ok := m.profiles[*profile]
	if !ok {
		return &ProfileInfo{
			Dependencies: &libsveltosset.Set{},
			Dependents:   &libsveltosset.Set{},
		}
	}

	return &profileInfo
}

func (m *instance) setProfileInfo(profile *corev1.ObjectReference, profileInfo *ProfileInfo) {
	m.profiles[*profile] = *profileInfo
}

func (m *instance) getCopyOfProfiles() map[corev1.ObjectReference]ProfileInfo {
	profileCopy := map[corev1.ObjectReference]ProfileInfo{}
	m.profileMux.RLock()
	defer m.profileMux.RUnlock()

	for k := range m.profiles {
		profileCopy[k] = m.profiles[k]
	}

	return profileCopy
}

func (m *instance) getAccessibleClusterProfiles(ctx context.Context, user string,
	cachedProfiles, result map[corev1.ObjectReference]ProfileInfo) error {

	clusterProfiles := &configv1beta1.ClusterProfileList{}
	err := m.client.List(ctx, clusterProfiles)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to list ClusterProfiles: %v", err))
		return err
	}

	for i := range clusterProfiles.Items {
		cp := &clusterProfiles.Items[i]
		ok, err := m.canGetClusterProfile(cp.Name, user)
		if err != nil {
			continue
		}
		if ok {
			profileInfo := getKeyFromObject(m.scheme, cp)
			if v, ok := cachedProfiles[*profileInfo]; ok {
				result[*profileInfo] = v
			}
		}
	}

	return nil
}

func (m *instance) getAccessibleProfiles(ctx context.Context, user string,
	cachedProfiles, result map[corev1.ObjectReference]ProfileInfo) error {

	// If user cannot list all Profiles, run a List so to get only Profiles user has access to
	// List (vs using m.profiles) is intentionally done to avoid taking lock for too long
	profiles := &configv1beta1.ProfileList{}
	err := m.client.List(ctx, profiles)
	if err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to list Profiles: %v", err))
		return err
	}

	for i := range profiles.Items {
		p := &profiles.Items[i]
		ok, err := m.canGetProfile(p.Namespace, p.Name, user)
		if err != nil {
			continue
		}
		if ok {
			profileInfo := getKeyFromObject(m.scheme, p)
			if v, ok := cachedProfiles[*profileInfo]; ok {
				result[*profileInfo] = v
			}
		}
	}

	return nil
}
