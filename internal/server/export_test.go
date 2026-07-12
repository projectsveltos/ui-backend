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

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
)

// Accessors for the private clusterCounts struct, used by external test packages.
func (cc clusterCounts) CAPITotal() int       { return cc.capiTotal }
func (cc clusterCounts) CAPINotReady() int    { return cc.capiNotReady }
func (cc clusterCounts) SveltosTotal() int    { return cc.sveltosTotal }
func (cc clusterCounts) SveltosNotReady() int { return cc.sveltosNotReady }
func (cc clusterCounts) PullMode() int        { return cc.pullMode }

// CountClusters is a test helper that bypasses SAR by accepting explicit canList booleans.
func (m *instance) CountClusters(ctx context.Context, canListSveltos, canListCAPI bool, user string) (clusterCounts, error) {
	sveltos, err := m.GetManagedSveltosClusters(ctx, canListSveltos, user)
	if err != nil {
		return clusterCounts{}, err
	}
	capi, err := m.GetManagedCAPIClusters(ctx, canListCAPI, user)
	if err != nil {
		return clusterCounts{}, err
	}

	cc := clusterCounts{
		capiTotal:    len(capi),
		sveltosTotal: len(sveltos),
	}
	for _, info := range capi {
		if !info.Ready {
			cc.capiNotReady++
		}
	}
	for _, info := range sveltos {
		if !info.Ready {
			cc.sveltosNotReady++
		}
		if info.PullMode {
			cc.pullMode++
		}
	}
	return cc, nil
}

// CountProfilesByKind is a test helper that bypasses SAR by accepting explicit canList booleans.
func (m *instance) CountProfilesByKind(ctx context.Context, canListCP, canListP bool, user string) (clusterProfiles, profiles int, err error) {
	accessible, err := m.GetProfiles(ctx, canListCP, canListP, user)
	if err != nil {
		return 0, 0, err
	}
	for ref := range accessible {
		switch ref.Kind {
		case configv1beta1.ClusterProfileKind:
			clusterProfiles++
		case configv1beta1.ProfileKind:
			profiles++
		}
	}
	return clusterProfiles, profiles, nil
}

// CountClusterSummaries returns the number of ClusterSummaries in the in-memory cache.
func (m *instance) CountClusterSummaries() int {
	return len(m.GetClusterProfileStatuses())
}

var (
	GetClustersInRange    = getClustersInRange
	GetHelmReleaseInRange = getHelmReleaseInRange
	GetResourcesInRange   = getResourcesInRange

	SortResources  = sortResources
	SortHelmCharts = sortHelmCharts

	ExamineClusterConditions = examineClusterConditions

	GetEventClusterDetails = getEventClusterDetails

	GetProfileData = getProfileData

	MatchingClustersFromClassifierReports                  = matchingClustersFromClassifierReports
	MatchingClustersFromManagementClusterClassifierReports = matchingClustersFromManagementClusterClassifierReports
	ClassifierReportsMatchCluster                          = classifierReportsMatchCluster
	ManagementClusterClassifierReportsMatchCluster         = managementClusterClassifierReportsMatchCluster
	CountMatchingClassifierReports                         = countMatchingClassifierReports
	CountMatchingManagementClusterClassifierReports        = countMatchingManagementClusterClassifierReports
	ClassifierNameMatches                                  = classifierNameMatches
)

var (
	GetClusterFiltersFromQuery = getClusterFiltersFromQuery
)

type (
	ProfileFilters    = profileFilters
	ClassifierFilters = classifierFilters
)

func GetNamespaceFilter(f clusterFilters) string {
	return f.Namespace
}

func GetNameFilter(f clusterFilters) string {
	return f.Name
}

func GetLabelFilter(f clusterFilters) string {
	return f.labelSelector.String()
}
