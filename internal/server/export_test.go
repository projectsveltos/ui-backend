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

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
)

// NewTestInstance builds a standalone *instance for tests that need a specific client, bypassing
// the InitializeManagerInstance singleton (which only ever initializes once per test binary, so
// it cannot be used to inject per-test fixtures for client-dependent methods).
func NewTestInstance(c client.Client, logger logr.Logger) *instance {
	return &instance{client: c, logger: logger}
}

// NewTestInstanceWithConfig is like NewTestInstance but also sets config, which the
// canList*/canGet* SubjectAccessReview checks need to build a real clientset. Used by tests
// that exercise real RBAC evaluation against an envtest apiserver.
func NewTestInstanceWithConfig(cfg *rest.Config, c client.Client, logger logr.Logger) *instance {
	return &instance{config: cfg, client: c, logger: logger}
}

// NewTestInstanceWithOIDCProxy is like NewTestInstanceWithConfig but also sets the OIDC proxy
// host/CA file, for tests that verify getKubernetesRestConfig's routing logic (see
// GetKubernetesRestConfig below).
func NewTestInstanceWithOIDCProxy(cfg *rest.Config, oidcProxyHost, oidcProxyCAFile string,
	c client.Client, logger logr.Logger,
) *instance {

	return &instance{
		config: cfg, oidcProxyHost: oidcProxyHost, oidcProxyCAFile: oidcProxyCAFile,
		client: c, logger: logger,
	}
}

// GetKubernetesRestConfig exposes getKubernetesRestConfig for tests.
func (m *instance) GetKubernetesRestConfig(token string) (*rest.Config, error) {
	return m.getKubernetesRestConfig(token)
}

// Exports of the canList* SubjectAccessReview checks, one per resource type, for tests that
// verify the Resource field (and Groups) sent matches a real RBAC rule (see k8s_utils_test.go).
func (m *instance) CanListSveltosClusters(user string, groups []string) (bool, error) {
	return m.canListSveltosClusters(user, groups)
}
func (m *instance) CanListCAPIClusters(user string, groups []string) (bool, error) {
	return m.canListCAPIClusters(user, groups)
}
func (m *instance) CanListClusterProfiles(user string, groups []string) (bool, error) {
	return m.canListClusterProfiles(user, groups)
}
func (m *instance) CanListProfiles(user string, groups []string) (bool, error) {
	return m.canListProfiles(user, groups)
}
func (m *instance) CanListClusterSummaries(user string, groups []string) (bool, error) {
	return m.canListClusterSummaries(user, groups)
}
func (m *instance) CanListEventTriggers(user string, groups []string) (bool, error) {
	return m.canListEventTriggers(user, groups)
}
func (m *instance) CanListClassifiers(user string, groups []string) (bool, error) {
	return m.canListClassifiers(user, groups)
}
func (m *instance) CanListManagementClusterClassifiers(user string, groups []string) (bool, error) {
	return m.canListManagementClusterClassifiers(user, groups)
}

// IsCAPIInstalled exposes isCAPIInstalled for tests.
func (m *instance) IsCAPIInstalled(ctx context.Context) (bool, error) {
	return m.isCAPIInstalled(ctx)
}

// CreateProfileObject exposes createProfileObject for tests. writeClient is passed explicitly
// (rather than derived from a token via GetImpersonatedClient) so tests can exercise the write
// path against a fake client without a live apiserver to token-review against.
func (m *instance) CreateProfileObject(ctx context.Context, writeClient client.Client,
	req *CreateProfileRequest) error {

	return m.createProfileObject(ctx, writeClient, req)
}

// UpdateProfileObject exposes updateProfileObject for tests.
func (m *instance) UpdateProfileObject(ctx context.Context, writeClient client.Client,
	req *UpdateProfileRequest) error {

	return m.updateProfileObject(ctx, writeClient, req)
}

// DeleteProfileObject exposes deleteProfileObject for tests.
func (m *instance) DeleteProfileObject(ctx context.Context, writeClient client.Client,
	req *DeleteProfileRequest) error {

	return m.deleteProfileObject(ctx, writeClient, req)
}

// ProfileExists exposes profileExists for tests.
func (m *instance) ProfileExists(ctx context.Context, id ProfileIdentity) (bool, error) {
	return m.profileExists(ctx, id)
}

// GetImpersonatedClient exposes getImpersonatedClient for tests.
func (m *instance) GetImpersonatedClient(token string) (client.Client, error) {
	return m.getImpersonatedClient(token)
}

var (
	ValidateProfileIdentity   = validateProfileIdentity
	ValidateCreateContent     = validateCreateContent
	ContentConfigMapNamespace = contentConfigMapNamespace
	ContentConfigMapName      = contentConfigMapName
	IsRemoteHelmSource        = isRemoteHelmSource
)

// CapiClusterCRDName exposes capiClusterCRDName for tests.
var CapiClusterCRDName = capiClusterCRDName

// GetHelmChartsForCluster exposes the unexported getHelmChartsForCluster for tests, always
// passing no release filters.
func (m *instance) GetHelmChartsForCluster(ctx context.Context, namespace, name string,
	clusterType libsveltosv1beta1.ClusterType) ([]HelmRelease, error) {

	return m.getHelmChartsForCluster(ctx, namespace, name, clusterType, nil)
}

// Accessors for the private clusterCounts struct, used by external test packages.
func (cc clusterCounts) CAPITotal() int       { return cc.capiTotal }
func (cc clusterCounts) CAPINotReady() int    { return cc.capiNotReady }
func (cc clusterCounts) SveltosTotal() int    { return cc.sveltosTotal }
func (cc clusterCounts) SveltosNotReady() int { return cc.sveltosNotReady }
func (cc clusterCounts) PullMode() int        { return cc.pullMode }

// CountClusters is a test helper that bypasses SAR by accepting explicit canList booleans.
func (m *instance) CountClusters(ctx context.Context, canListSveltos, canListCAPI bool, user string,
	groups []string) (clusterCounts, error) {

	sveltos, err := m.GetManagedSveltosClusters(ctx, canListSveltos, user, groups)
	if err != nil {
		return clusterCounts{}, err
	}
	capi, err := m.GetManagedCAPIClusters(ctx, canListCAPI, user, groups)
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
func (m *instance) CountProfilesByKind(ctx context.Context, canListCP, canListP bool, user string,
	groups []string) (clusterProfiles, profiles int, err error) {

	accessible, err := m.GetProfiles(ctx, canListCP, canListP, user, groups)
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
	AbortMCPError              = abortMCPError
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
