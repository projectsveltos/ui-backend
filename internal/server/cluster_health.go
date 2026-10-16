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
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/projectsveltos/addon-controller/lib/healthmonitoring"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
)

// ClusterHealthProfile identifies a profile owning the HealthCheck that reported a resource.
type ClusterHealthProfile struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// ClusterHealthResource is a resource of a cluster that is not Healthy.
type ClusterHealthResource struct {
	Group     string                         `json:"group"`
	Version   string                         `json:"version"`
	Kind      string                         `json:"kind"`
	Namespace string                         `json:"namespace,omitempty"`
	Name      string                         `json:"name"`
	Status    libsveltosv1beta1.HealthStatus `json:"status"`
	Message   string                         `json:"message,omitempty"`
	// Profiles are the profiles owning the HealthCheck that reported the resource.
	Profiles []ClusterHealthProfile `json:"profiles"`
	// HealthCheckName is the HealthCheck that reported the resource. Set only when no profile owns it.
	HealthCheckName string `json:"healthCheckName,omitempty"`
}

// ClusterHealthResult is the response of /clusterHealth.
type ClusterHealthResult struct {
	// State is the health state of the cluster: NotEnabled (no profile monitors it), NoData,
	// Healthy, Suspended, Progressing or Degraded.
	State          healthmonitoring.State  `json:"state"`
	TotalResources int                     `json:"totalResources"`
	Resources      []ClusterHealthResource `json:"resources"`
}

// healthProfileKey identifies a profile within a cluster. A ClusterProfile and a Profile with
// the same name differ by kind.
type healthProfileKey struct {
	kind string
	name string
}

// getClusterHealthResult returns the health of a cluster: the not Healthy resources, filtered
// and paginated, worst status first. The profile filters match exactly; a resource no profile
// owns never matches a profile filter.
func (m *instance) getClusterHealthResult(clusterType libsveltosv1beta1.ClusterType,
	clusterNamespace, clusterName string, profile *profileFilters, resource *resourceFilters,
	limit, skip int) (*ClusterHealthResult, error) {

	health := m.GetClusterHealth(clusterType, clusterNamespace, clusterName)

	resources := filterClusterHealthResources(newClusterHealthResources(health.Resources), profile, resource)

	page, err := getSliceInRange(resources, limit, skip)
	if err != nil {
		return nil, err
	}
	if page == nil {
		page = make([]ClusterHealthResource, 0)
	}

	return &ClusterHealthResult{State: health.State, TotalResources: len(resources), Resources: page}, nil
}

func newClusterHealthResources(unhealthy []healthmonitoring.UnhealthyResource) []ClusterHealthResource {
	resources := make([]ClusterHealthResource, 0, len(unhealthy))
	for i := range unhealthy {
		group, version := splitAPIVersion(unhealthy[i].ObjectRef.APIVersion)

		profiles := make([]ClusterHealthProfile, 0, len(unhealthy[i].Profiles))
		for _, profile := range unhealthy[i].Profiles {
			profiles = append(profiles, ClusterHealthProfile{
				Kind: profile.Kind, Namespace: profile.Namespace, Name: profile.Name,
			})
		}

		resources = append(resources, ClusterHealthResource{
			Group:           group,
			Version:         version,
			Kind:            unhealthy[i].ObjectRef.Kind,
			Namespace:       unhealthy[i].ObjectRef.Namespace,
			Name:            unhealthy[i].ObjectRef.Name,
			Status:          unhealthy[i].Status,
			Message:         unhealthy[i].Message,
			Profiles:        profiles,
			HealthCheckName: unhealthy[i].HealthCheckName,
		})
	}
	return resources
}

func splitAPIVersion(apiVersion string) (group, version string) {
	groupVersion, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return "", apiVersion
	}
	return groupVersion.Group, groupVersion.Version
}

func filterClusterHealthResources(resources []ClusterHealthResource, profile *profileFilters,
	resource *resourceFilters) []ClusterHealthResource {

	filtered := make([]ClusterHealthResource, 0, len(resources))
	for i := range resources {
		if matchesResourceFilters(&resources[i], resource) && matchesProfileFilters(&resources[i], profile) {
			filtered = append(filtered, resources[i])
		}
	}
	return filtered
}

// matchesResourceFilters follows /resources: a case insensitive substring match.
func matchesResourceFilters(health *ClusterHealthResource, filters *resourceFilters) bool {
	return containsFold(health.Kind, filters.Kind) &&
		containsFold(health.Namespace, filters.Namespace) &&
		containsFold(health.Name, filters.Name)
}

func containsFold(value, filter string) bool {
	return strings.Contains(strings.ToLower(value), strings.ToLower(filter))
}

func matchesProfileFilters(health *ClusterHealthResource, filters *profileFilters) bool {
	if filters.Kind == "" && filters.Namespace == "" && filters.Name == "" {
		return true
	}

	for _, profile := range health.Profiles {
		if (filters.Kind == "" || strings.EqualFold(profile.Kind, filters.Kind)) &&
			(filters.Namespace == "" || profile.Namespace == filters.Namespace) &&
			(filters.Name == "" || profile.Name == filters.Name) {

			return true
		}
	}
	return false
}

// profileHealthStates returns the health state of each profile with monitoring enabled.
func profileHealthStates(health *healthmonitoring.ClusterHealth) map[healthProfileKey]healthmonitoring.State {
	states := make(map[healthProfileKey]healthmonitoring.State, len(health.Profiles))
	for i := range health.Profiles {
		profile := &health.Profiles[i]
		states[healthProfileKey{kind: profile.Profile.Kind, name: profile.Profile.Name}] = profile.State
	}
	return states
}

// setProfileHealthStatus sets the health state on each row whose profile has monitoring enabled.
func setProfileHealthStatus(rows []ProfileStatusResult, states map[healthProfileKey]healthmonitoring.State) {
	for i := range rows {
		state, ok := states[healthProfileKey{kind: rows[i].ProfileType, name: rows[i].ProfileName}]
		if ok {
			rows[i].HealthStatus = state
		}
	}
}
