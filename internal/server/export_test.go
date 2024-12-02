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

var (
	GetClustersInRange    = getClustersInRange
	GetHelmReleaseInRange = getHelmReleaseInRange
	GetResourcesInRange   = getResourcesInRange

	SortResources  = sortResources
	SortHelmCharts = sortHelmCharts
)

var (
	GetClusterFiltersFromQuery = getClusterFiltersFromQuery
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
