package server

import (
	configv1alpha1 "github.com/projectsveltos/addon-controller/api/v1alpha1"
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
	if cfs.Status != configv1alpha1.FeatureStatusProvisioned &&
		cfs.Status != configv1alpha1.FeatureStatusRemoved {

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
