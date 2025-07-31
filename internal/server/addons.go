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
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
)

type HelmRelease struct {
	// RepoURL URL of the repo containing the helm chart deployed
	// in the Cluster.
	// +kubebuilder:validation:MinLength=1
	RepoURL string `json:"repoURL"`

	// ReleaseName name of the release deployed in the Cluster.
	// +kubebuilder:validation:MinLength=1
	ReleaseName string `json:"releaseName"`

	// Namespace where chart is deployed in the Cluster.
	Namespace string `json:"namespace"`

	// ChartVersion is the version of the helm chart deployed in the Cluster.
	ChartVersion string `json:"chartVersion"`

	// The URL to an icon file.
	Icon string `json:"icon"`

	// LastAppliedTime identifies when this resource was last applied to the cluster.
	LastAppliedTime *metav1.Time `json:"lastAppliedTime"`

	// ProfileName is the name of the ClusterProfile/Profile that
	// caused the helm chart to be deployed
	ProfileName string `json:"profileName"`
}

type Resource struct {
	// Name of the resource deployed in the Cluster.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Namespace of the resource deployed in the Cluster.
	// Empty for resources scoped at cluster level.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Group of the resource deployed in the Cluster.
	Group string `json:"group"`

	// Kind of the resource deployed in the Cluster.
	// +kubebuilder:validation:MinLength=1
	Kind string `json:"kind"`

	// Version of the resource deployed in the Cluster.
	// +kubebuilder:validation:MinLength=1
	Version string `json:"version"`

	// LastAppliedTime identifies when this resource was last applied to the cluster.
	// +optional
	LastAppliedTime *metav1.Time `json:"lastAppliedTime,omitempty"`

	// ProfileNames is a slice of the names of the ClusterProfile/Profile instances
	// that caused the helm chart to be deployed
	ProfileNames []string `json:"profileNames"`
}

type HelmReleaseResult struct {
	TotalHelmReleases int           `json:"totalHelmReleases"`
	HelmReleases      []HelmRelease `json:"helmReleases"`
}

type ResourceResult struct {
	TotalResources int        `json:"totalResources"`
	Resources      []Resource `json:"resources"`
}

func (m *instance) getHelmChartsForCluster(ctx context.Context, namespace, name string,
	clusterType libsveltosv1beta1.ClusterType) ([]HelmRelease, error) {

	// Even though only one ClusterConfiguration exists for a given cluster,
	// we are doing a list vs a Get because how to build name of a ClusterConfiguration
	// is not exposed
	clusterConfigurations := &configv1beta1.ClusterConfigurationList{}
	listOptions := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			configv1beta1.ClusterNameLabel: name,
			configv1beta1.ClusterTypeLabel: string(clusterType),
		},
	}

	err := m.client.List(ctx, clusterConfigurations, listOptions...)
	if err != nil {
		return nil, err
	}

	if len(clusterConfigurations.Items) > 1 {
		return nil, fmt.Errorf("found one more than one ClusterConfiguration for cluster")
	}

	if len(clusterConfigurations.Items) == 0 {
		return nil, nil
	}

	// Only one returned
	cc := &clusterConfigurations.Items[0]
	return getHelmReleases(cc), nil
}

func (m *instance) getResourcesForCluster(ctx context.Context, namespace, name string,
	clusterType libsveltosv1beta1.ClusterType) ([]Resource, error) {

	// Even though only one ClusterConfiguration exists for a given cluster,
	// we are doing a list vs a Get because how to build name of a ClusterConfiguration
	// is not exposed
	clusterConfigurations := &configv1beta1.ClusterConfigurationList{}
	listOptions := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels{
			configv1beta1.ClusterNameLabel: name,
			configv1beta1.ClusterTypeLabel: string(clusterType),
		},
	}

	err := m.client.List(ctx, clusterConfigurations, listOptions...)
	if err != nil {
		return nil, err
	}

	if len(clusterConfigurations.Items) > 1 {
		return nil, fmt.Errorf("found one more than one ClusterConfiguration for cluster")
	}

	if len(clusterConfigurations.Items) == 0 {
		return nil, nil
	}

	// Only one returned
	cc := &clusterConfigurations.Items[0]
	resources := getResources(cc)

	result := make([]Resource, len(resources))
	i := 0
	for r := range resources {
		result[i] = Resource{
			Name:            r.Name,
			Namespace:       r.Namespace,
			Group:           r.Group,
			Kind:            r.Kind,
			Version:         r.Version,
			LastAppliedTime: r.LastAppliedTime,
			ProfileNames:    resources[r],
		}
		i++
	}
	return result, nil
}

// getHelmReleases returns list of helm releases deployed in a given cluster
func getHelmReleases(clusterConfiguration *configv1beta1.ClusterConfiguration,
) []HelmRelease {

	results := make([]HelmRelease, 0)

	for i := range clusterConfiguration.Status.ClusterProfileResources {
		r := clusterConfiguration.Status.ClusterProfileResources[i]
		results = append(results,
			addDeployedCharts(configv1beta1.ClusterProfileKind, r.ClusterProfileName, r.Features)...)
	}
	for i := range clusterConfiguration.Status.ProfileResources {
		r := clusterConfiguration.Status.ProfileResources[i]
		results = append(results,
			addDeployedCharts(configv1beta1.ProfileKind, r.ProfileName, r.Features)...)
	}

	return results
}

func addDeployedCharts(profileKind, profileName string, features []configv1beta1.Feature,
) []HelmRelease {

	results := make([]HelmRelease, 0)
	for i := range features {
		results = append(results, addDeployedChartsForFeature(
			fmt.Sprintf("%s/%s", profileKind, profileName), features[i].Charts)...)
	}

	return results
}

func addDeployedChartsForFeature(profileName string, charts []configv1beta1.Chart,
) []HelmRelease {

	results := make([]HelmRelease, 0)

	for i := range charts {
		chart := &charts[i]
		results = append(results,
			HelmRelease{
				RepoURL:         chart.RepoURL,
				ReleaseName:     chart.ReleaseName,
				Namespace:       chart.Namespace,
				ChartVersion:    chart.ChartVersion,
				Icon:            chart.Icon,
				LastAppliedTime: chart.LastAppliedTime,
				ProfileName:     profileName,
			})
	}

	return results
}

// getResources returns list of resources deployed in a given cluster
func getResources(clusterConfiguration *configv1beta1.ClusterConfiguration,
) map[libsveltosv1beta1.Resource][]string {

	results := make(map[libsveltosv1beta1.Resource][]string)

	for i := range clusterConfiguration.Status.ClusterProfileResources {
		r := clusterConfiguration.Status.ClusterProfileResources[i]
		addDeployedResources(configv1beta1.ClusterProfileKind, r.ClusterProfileName, r.Features, results)
	}
	for i := range clusterConfiguration.Status.ProfileResources {
		r := clusterConfiguration.Status.ProfileResources[i]
		addDeployedResources(configv1beta1.ProfileKind, r.ProfileName, r.Features, results)
	}

	return results
}

func addDeployedResources(profilesKind, profileName string,
	features []configv1beta1.Feature, results map[libsveltosv1beta1.Resource][]string) {

	for i := range features {
		addDeployedResourcesForFeature(
			fmt.Sprintf("%s/%s", profilesKind, profileName),
			features[i].Resources, results)
	}
}

func addDeployedResourcesForFeature(profileName string,
	resources []libsveltosv1beta1.Resource, results map[libsveltosv1beta1.Resource][]string) {

	for i := range resources {
		resource := &resources[i]
		if v, ok := results[*resource]; ok {
			v = append(v, profileName)
			results[*resource] = v
		} else {
			results[*resource] = []string{profileName}
		}
	}
}

func getSliceInRange[T any](items []T, limit, skip int) ([]T, error) {
	if skip < 0 {
		return nil, errors.New("skip cannot be negative")
	}
	if limit < 0 {
		return nil, errors.New("limit cannot be negative")
	}
	if skip >= len(items) {
		return nil, nil
	}

	// Adjust limit based on slice length and skip
	adjustedLimit := limit
	if skip+limit > len(items) {
		adjustedLimit = len(items) - skip
	}

	// Use slicing to extract the desired sub-slice
	return items[skip : skip+adjustedLimit], nil
}

func getHelmReleaseInRange(helmReleases []HelmRelease, limit, skip int) ([]HelmRelease, error) {
	return getSliceInRange(helmReleases, limit, skip)
}

func getResourcesInRange(resources []Resource, limit, skip int) ([]Resource, error) {
	return getSliceInRange(resources, limit, skip)
}

// sortResources sorts resources by last applied time. In case time is same,
// resources are sorted by GVK
func sortResources(resources []Resource, i, j int) bool {
	if resources[i].LastAppliedTime.Equal(resources[j].LastAppliedTime) {
		// If deployment time is same, sort by GVK
		gvk1 := schema.GroupVersionKind{
			Group:   resources[i].Group,
			Kind:    resources[i].Kind,
			Version: resources[i].Version,
		}

		gvk2 := schema.GroupVersionKind{
			Group:   resources[j].Group,
			Kind:    resources[j].Kind,
			Version: resources[j].Version,
		}

		return gvk1.String() < gvk2.String()
	}

	return resources[i].LastAppliedTime.Before(resources[j].LastAppliedTime)
}

// sortHelmCharts sorts helm charts by last applied time. In case time is same,
// resources are sorted by namespace and finally release name
func sortHelmCharts(helmCharts []HelmRelease, i, j int) bool {
	if helmCharts[i].LastAppliedTime.Equal(helmCharts[j].LastAppliedTime) {
		// If deployment time is same, sort by release namespace and then release name
		if helmCharts[i].Namespace == helmCharts[j].Namespace {
			return helmCharts[i].ReleaseName < helmCharts[j].ReleaseName
		}

		return helmCharts[i].Namespace < helmCharts[j].Namespace
	}

	return helmCharts[i].LastAppliedTime.Before(helmCharts[j].LastAppliedTime)
}
