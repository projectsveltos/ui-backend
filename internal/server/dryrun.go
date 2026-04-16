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
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
)

const (
	noAction = "No Action"
)

// DryRunHelmChange describes a simulated Helm release action.
type DryRunHelmChange struct {
	ReleaseName      string `json:"releaseName"`
	ReleaseNamespace string `json:"releaseNamespace"`
	ChartVersion     string `json:"chartVersion,omitempty"`
	Action           string `json:"action"`
	Message          string `json:"message,omitempty"`
}

// DryRunResourceChange describes a simulated Kubernetes resource action.
type DryRunResourceChange struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind"`
	Version   string `json:"version"`
	Action    string `json:"action"`
	Message   string `json:"message,omitempty"`
}

// DryRunResult holds the simulated changes for a profile+cluster pair.
// ReportMissing is true when the controller has not yet generated a
// ClusterReport for this profile (e.g. the profile was just created).
type DryRunResult struct {
	ProfileName string `json:"profileName"`
	ProfileKind string `json:"profileKind"`
	// Namespace is only set for namespaced Profile resources.
	Namespace string `json:"namespace,omitempty"`
	// HasChanges is true when at least one actionable change was found.
	HasChanges bool `json:"hasChanges"`
	// ReportMissing is true when no ClusterReport exists yet.
	ReportMissing      bool                   `json:"reportMissing,omitempty"`
	HelmReleaseChanges []DryRunHelmChange     `json:"helmReleaseChanges,omitempty"`
	ResourceChanges    []DryRunResourceChange `json:"resourceChanges,omitempty"`
	KustomizeChanges   []DryRunResourceChange `json:"kustomizeChanges,omitempty"`
}

// getDryRunChanges fetches the ClusterReport for the given profile+cluster pair
// and returns the structured diff.
func (m *instance) getDryRunChanges(
	ctx context.Context,
	profileKind, profileNamespace, profileName string,
	clusterNamespace, clusterName string,
	clusterType libsveltosv1beta1.ClusterType,
) (*DryRunResult, error) {

	result := &DryRunResult{
		ProfileName: profileName,
		ProfileKind: profileKind,
		Namespace:   profileNamespace,
	}

	reportName := dryRunClusterReportName(profileKind, profileName, clusterName, clusterType)
	cr := &configv1beta1.ClusterReport{}
	err := m.client.Get(ctx, types.NamespacedName{Namespace: clusterNamespace, Name: reportName}, cr)
	if err != nil {
		if apierrors.IsNotFound(err) {
			result.ReportMissing = true
			return result, nil
		}
		return nil, fmt.Errorf("failed to fetch ClusterReport %s/%s: %w", clusterNamespace, reportName, err)
	}

	result.HelmReleaseChanges = helmChangesFromReport(cr)
	result.ResourceChanges = resourceChangesFromReports(cr.Status.ResourceReports, cr.Status.HelmResourceReports)
	result.KustomizeChanges = resourceChangesFromReports(cr.Status.KustomizeResourceReports, nil)
	result.HasChanges = len(result.HelmReleaseChanges) > 0 ||
		len(result.ResourceChanges) > 0 ||
		len(result.KustomizeChanges) > 0

	return result, nil
}

// dryRunClusterReportName computes the ClusterReport name for a profile+cluster pair.
// This mirrors the formula used in addon-controller and mcp-server.
func dryRunClusterReportName(
	profileKind, profileName, clusterName string,
	clusterType libsveltosv1beta1.ClusterType,
) string {

	prefix := ""
	if profileKind == configv1beta1.ProfileKind {
		prefix = "p--"
	}
	return prefix + profileName + "--" + strings.ToLower(string(clusterType)) + "--" + clusterName
}

func helmChangesFromReport(cr *configv1beta1.ClusterReport) []DryRunHelmChange {
	var changes []DryRunHelmChange
	for i := range cr.Status.ReleaseReports {
		r := &cr.Status.ReleaseReports[i]
		if r.Action == noAction {
			continue
		}
		changes = append(changes, DryRunHelmChange{
			ReleaseName:      r.ReleaseName,
			ReleaseNamespace: r.ReleaseNamespace,
			ChartVersion:     r.ChartVersion,
			Action:           r.Action,
			Message:          r.Message,
		})
	}
	return changes
}

func resourceChangesFromReports(
	primary, secondary []libsveltosv1beta1.ResourceReport,
) []DryRunResourceChange {

	var changes []DryRunResourceChange
	for _, reports := range [][]libsveltosv1beta1.ResourceReport{primary, secondary} {
		for i := range reports {
			r := &reports[i]
			if r.Action == noAction {
				continue
			}
			changes = append(changes, DryRunResourceChange{
				Name:      r.Resource.Name,
				Namespace: r.Resource.Namespace,
				Group:     r.Resource.Group,
				Kind:      r.Resource.Kind,
				Version:   r.Resource.Version,
				Action:    r.Action,
				Message:   r.Message,
			})
		}
	}
	return changes
}
