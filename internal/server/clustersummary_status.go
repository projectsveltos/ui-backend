/*
Copyright 2024-2025. projectsveltos.io. All rights reserved.

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
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
)

type ProfileStatusResult struct {
	ProfileName string `json:"profileName"`
	ProfileType string `json:"profileType"`
	ClusterFeatureSummary
}

func getFlattenedProfileStatusesInRange(flattenedProfileStatuses []ProfileStatusResult, limit, skip int) ([]ProfileStatusResult, error) {
	return getSliceInRange(flattenedProfileStatuses, limit, skip)
}

func flattenProfileStatuses(profileStatuses []ClusterProfileStatus, failedOnly bool) []ProfileStatusResult {
	result := make([]ProfileStatusResult, 0)

	for i := range profileStatuses {
		result = append(result, flattenProfileStatus(&profileStatuses[i], failedOnly)...)
	}

	return result
}

func flattenProfileStatus(profileStatus *ClusterProfileStatus, failedOnly bool) []ProfileStatusResult {
	result := make([]ProfileStatusResult, 0)
	for i := range profileStatus.Summary {
		if !failedOnly || !isCompleted(profileStatus.Summary[i]) {
			result = append(result,
				ProfileStatusResult{
					ProfileName:           profileStatus.ProfileName,
					ProfileType:           profileStatus.ProfileType,
					ClusterFeatureSummary: profileStatus.Summary[i],
				})
		}
	}

	return result
}

func isCompleted(cfs ClusterFeatureSummary) bool {
	if cfs.Status != libsveltosv1beta1.FeatureStatusProvisioned &&
		cfs.Status != libsveltosv1beta1.FeatureStatusRemoved {

		return false
	}

	return true
}

// sortClusterProfileStatus sort by ProfileType first, ProfileName later and finally by FeatureID
func sortClusterProfileStatus(flattenedProfileStatuses []ProfileStatusResult, i, j int) bool {
	if flattenedProfileStatuses[i].ProfileType == flattenedProfileStatuses[j].ProfileType {
		if flattenedProfileStatuses[i].ProfileName == flattenedProfileStatuses[j].ProfileName {
			return flattenedProfileStatuses[i].FeatureID < flattenedProfileStatuses[j].FeatureID
		}

		return flattenedProfileStatuses[i].ProfileName < flattenedProfileStatuses[j].ProfileName
	}

	return flattenedProfileStatuses[i].ProfileType < flattenedProfileStatuses[j].ProfileType
}
