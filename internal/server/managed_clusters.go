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
	"errors"
)

type ManagedCluster struct {
	Namespace   string `json:"namespace"`
	Name        string `json:"name"`
	ClusterInfo `json:"clusterInfo"`
}

type ManagedClusters []ManagedCluster

func (s ManagedClusters) Len() int      { return len(s) }
func (s ManagedClusters) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s ManagedClusters) Less(i, j int) bool {
	if s[i].Namespace == s[j].Namespace {
		return s[i].Name < s[j].Name
	}
	return s[i].Namespace < s[j].Namespace
}

func getLimitedClusters(clusters ManagedClusters, limit, skip int) (ManagedClusters, error) {
	if skip < 0 {
		return nil, errors.New("skip cannot be negative")
	}
	if limit < 0 {
		return nil, errors.New("limit cannot be negative")
	}
	if skip >= len(clusters) {
		return nil, errors.New("skip cannot be greater than or equal to the length of the slice")
	}

	// Adjust limit based on slice length and skip
	adjustedLimit := limit
	if skip+limit > len(clusters) {
		adjustedLimit = len(clusters) - skip
	}

	// Use slicing to extract the desired sub-slice
	return clusters[skip : skip+adjustedLimit], nil
}
