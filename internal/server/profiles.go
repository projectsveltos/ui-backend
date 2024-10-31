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
	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
)

type Profile struct {
	// Kind of the profile (ClusterProfile vs Profile)
	Kind string `json:"kind"`
	// Namespace of the profile (empty for ClusterProfile)
	Namespace string `json:"namespace"`
	// Name of the profile
	Name string `json:"name"`
	// List of profiles this profile depends on
	Dependencies []corev1.ObjectReference `json:"dependencies"`
	// List of profiles that depend on this profile
	Dependents []corev1.ObjectReference `json:"dependents"`
	// List of managed clusters matching this profile
	MatchingClusters []corev1.ObjectReference `json:"matchingClusters"`
	// Profile's Spec section
	Spec configv1beta1.Spec `json:"spec"`
}

type Profiles []Profile

type ProfileResult struct {
	TotalProfiles int      `json:"totalProfiles"`
	Profiles      Profiles `json:"profiles"`
}

func (s Profiles) Len() int      { return len(s) }
func (s Profiles) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s Profiles) Less(i, j int) bool {
	if s[i].Namespace == s[j].Namespace {
		return s[i].Name < s[j].Name
	}
	return s[i].Namespace < s[j].Namespace
}

type profileFilters struct {
	Namespace string `uri:"namespace"`
	Name      string `uri:"name"`
	Kind      string `uri:"kind"`
}

func getProfileFiltersFromQuery(c *gin.Context) *profileFilters {
	var filters profileFilters
	// Get the values from query parameters
	filters.Namespace = c.Query("namespace")
	filters.Name = c.Query("name")
	filters.Kind = c.Query("kind")

	return &filters
}
