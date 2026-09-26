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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
)

var (
	// ErrProfileAlreadyExists is returned by createProfileObject when a ClusterProfile/Profile
	// with the requested identity already exists - both from the upfront existence check (the
	// common, fast-path case) and from a race lost against the actual Create call (rare, but the
	// existence check alone cannot be relied on: see ENHANCEMENTS/profile-creation-dashboard.md).
	ErrProfileAlreadyExists = errors.New("a profile with this identity already exists")

	// ErrInvalidRequest wraps every request-shape validation failure (bad Kind, missing required
	// field, more/fewer than one content flow set, ...) so handlers can map it to 400 Bad Request
	// with errors.Is, distinct from ErrProfileAlreadyExists (409) and apiserver errors (403/404/500).
	ErrInvalidRequest = errors.New("invalid request")
)

const (
	// dashboardContentNamespace is the fixed namespace used for a dashboard-created
	// ClusterProfile's YAML-flow ConfigMap. A ClusterProfile's PolicyRef.Namespace, left empty,
	// resolves per-matching-cluster at deploy time, which is never what a single
	// dashboard-created source of truth wants.
	dashboardContentNamespace = "projectsveltos"

	// contentDataKey is the single data key used for a dashboard-created ConfigMap/Secret's
	// pasted YAML content and for referenced-content edits from PUT /profile, matching the
	// convention used throughout the dashboard/docs.addons.raw_yaml.md examples.
	contentDataKey = "content.yaml"
)

// invalidRequestf wraps a formatted message with ErrInvalidRequest.
func invalidRequestf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidRequest, fmt.Sprintf(format, args...))
}

// ProfileIdentity is the (Kind, Namespace, Name) triple identifying a ClusterProfile or Profile.
// Namespace is required for Profile and must be empty for ClusterProfile. Mirrors the existing
// Kind/Namespace convention from GET /profile (Profile.Kind in profiles.go).
type ProfileIdentity struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// HelmChartInput is the subset of configv1beta1.HelmChart the Create form exposes.
type HelmChartInput struct {
	RepositoryURL    string `json:"repositoryURL"`
	RepositoryName   string `json:"repositoryName,omitempty"`
	ChartName        string `json:"chartName,omitempty"`
	ChartVersion     string `json:"chartVersion,omitempty"`
	ReleaseNamespace string `json:"releaseNamespace"`
	ReleaseName      string `json:"releaseName"`
	Values           string `json:"values,omitempty"`
}

// ExistingContentRef points a profile's PolicyRefs at a ConfigMap/Secret that already exists,
// instead of creating a new one - the deliberate, first-class way to share content across
// profiles (see ENHANCEMENTS/profile-creation-dashboard.md for why this replaced an earlier
// OwnerReference-based ownership-tracking design).
type ExistingContentRef struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// SecretReferenceInput identifies an existing Secret by namespace and name. Like
// ExistingContentRef, this only references a Secret the caller has already created - the
// dashboard does not create or edit credentials.
type SecretReferenceInput struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// RemoteURLInput is the subset of configv1beta1.RemoteURL the Create form exposes: an
// HTTP(S)/OCI source PolicyRefs fetch directly, with no ConfigMap/Secret created for it. Interval
// is a Go duration string (e.g. "5m"); left empty, the CRD's own default applies.
type RemoteURLInput struct {
	URL                   string                `json:"url"`
	Interval              string                `json:"interval,omitempty"`
	SecretRef             *SecretReferenceInput `json:"secretRef,omitempty"`
	Template              bool                  `json:"template,omitempty"`
	InsecureSkipTLSVerify bool                  `json:"insecureSkipTLSVerify,omitempty"`
	PlainHTTP             bool                  `json:"plainHTTP,omitempty"`
}

// CreateProfileRequest is the body of POST /profile. Exactly one of HelmChart, YAML,
// ExistingContent or RemoteURL must be set.
type CreateProfileRequest struct {
	ProfileIdentity
	ClusterSelector map[string]string   `json:"clusterSelector"`
	Tier            *int32              `json:"tier,omitempty"`
	DependsOn       []string            `json:"dependsOn,omitempty"`
	HelmChart       *HelmChartInput     `json:"helmChart,omitempty"`
	YAML            *string             `json:"yaml,omitempty"`
	ExistingContent *ExistingContentRef `json:"existingContent,omitempty"`
	RemoteURL       *RemoteURLInput     `json:"remoteURL,omitempty"`
}

// UpdateProfileRequest is the body of PUT /profile. SpecYAML replaces the object's entire
// Spec. ReferencedContent optionally updates the data of ConfigMaps/Secrets the edited Spec's
// PolicyRefs point at, keyed by name.
type UpdateProfileRequest struct {
	ProfileIdentity
	SpecYAML          string            `json:"specYAML"`
	ReferencedContent map[string]string `json:"referencedContent,omitempty"`
}

// DeleteProfileRequest is the body of DELETE /profile.
type DeleteProfileRequest struct {
	ProfileIdentity
	RemoveReferencedContent bool `json:"removeReferencedContent,omitempty"`
}

// validateProfileIdentity checks the (Kind, Namespace, Name) triple all three write requests
// share. Namespace is required for Profile and is not checked/rejected for ClusterProfile -
// callers that need to reject a stray Namespace on a ClusterProfile request do so themselves.
func validateProfileIdentity(id ProfileIdentity) error {
	if id.Kind != configv1beta1.ClusterProfileKind && id.Kind != configv1beta1.ProfileKind {
		return invalidRequestf("kind must be %q or %q", configv1beta1.ClusterProfileKind, configv1beta1.ProfileKind)
	}
	if id.Name == "" {
		return invalidRequestf("%s", nameRequiredError)
	}
	if id.Kind == configv1beta1.ProfileKind && id.Namespace == "" {
		return invalidRequestf("%s", namespaceRequiredError)
	}
	return nil
}

// isRemoteHelmSource returns true if repositoryURL is a traditional HTTP(S) or OCI repository -
// the cases where RepositoryName is required, per the CRD's own CEL rule (api/v1beta1/spec.go).
// A Flux-source URL (gitrepository://, ocirepository://, bucket://) does not need it.
func isRemoteHelmSource(repositoryURL string) bool {
	lower := strings.ToLower(repositoryURL)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "oci://")
}

// validateCreateContent checks that exactly one content flow is set and that each flow's own
// required fields are present.
func validateCreateContent(req *CreateProfileRequest) error {
	set := 0
	if req.HelmChart != nil {
		set++
	}
	if req.YAML != nil {
		set++
	}
	if req.ExistingContent != nil {
		set++
	}
	if req.RemoteURL != nil {
		set++
	}
	if set != 1 {
		return invalidRequestf("exactly one of helmChart, yaml, existingContent, or remoteURL must be set")
	}

	switch {
	case req.HelmChart != nil:
		hc := req.HelmChart
		if hc.RepositoryURL == "" || hc.ReleaseName == "" || hc.ReleaseNamespace == "" {
			return invalidRequestf("helmChart requires repositoryURL, releaseName and releaseNamespace")
		}
		if isRemoteHelmSource(hc.RepositoryURL) && hc.RepositoryName == "" {
			return invalidRequestf("repositoryName is required when repositoryURL is http(s):// or oci://")
		}
	case req.ExistingContent != nil:
		ec := req.ExistingContent
		if ec.Name == "" || ec.Namespace == "" {
			return invalidRequestf("existingContent requires name and namespace")
		}
		if ec.Kind != string(libsveltosv1beta1.ConfigMapReferencedResourceKind) &&
			ec.Kind != string(libsveltosv1beta1.SecretReferencedResourceKind) {

			return invalidRequestf("existingContent.kind must be %q or %q",
				libsveltosv1beta1.ConfigMapReferencedResourceKind, libsveltosv1beta1.SecretReferencedResourceKind)
		}
	case req.RemoteURL != nil:
		return validateRemoteURL(req.RemoteURL)
	}

	return nil
}

// validateRemoteURL checks the fields of a RemoteURL content flow: the url scheme, that
// interval (if set) is a parseable Go duration, and that secretRef (if set) fully identifies a
// Secret.
func validateRemoteURL(ru *RemoteURLInput) error {
	lower := strings.ToLower(ru.URL)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") &&
		!strings.HasPrefix(lower, "oci://") {

		return invalidRequestf("remoteURL.url must start with http://, https://, or oci://")
	}
	if ru.Interval != "" {
		if _, err := time.ParseDuration(ru.Interval); err != nil {
			return invalidRequestf("remoteURL.interval is not a valid duration: %v", err)
		}
	}
	if ru.SecretRef != nil && (ru.SecretRef.Name == "" || ru.SecretRef.Namespace == "") {
		return invalidRequestf("remoteURL.secretRef requires namespace and name")
	}
	return nil
}

// contentConfigMapNamespace returns the namespace a dashboard-created YAML-flow ConfigMap must
// live in for the given profile identity - see the PolicyRef.Namespace doc comments in
// api/v1beta1/spec.go this mirrors.
func contentConfigMapNamespace(id ProfileIdentity) string {
	if id.Kind == configv1beta1.ProfileKind {
		return id.Namespace
	}
	return dashboardContentNamespace
}

// contentConfigMapName derives a ConfigMap name from the profile name, truncating and
// appending a short content hash if the natural name would exceed the 253-character DNS
// subdomain limit. profileName is also hashed (not just truncated) so two names that only
// differ after the truncation point don't collide.
func contentConfigMapName(profileName string) string {
	const maxNameLength = 253
	const suffix = "-content"

	name := strings.ToLower(profileName) + suffix
	if len(name) <= maxNameLength {
		return name
	}

	sum := sha256.Sum256([]byte(profileName))
	const hashLen = 8
	hash := hex.EncodeToString(sum[:])[:hashLen]

	truncated := name[:maxNameLength-len(hash)-1]
	return fmt.Sprintf("%s-%s", truncated, hash)
}

// policyRefKindFor maps an ExistingContentRef/HelmChart-flow content kind to the PolicyRef.Kind
// string - currently identical, kept as a named conversion point in case that changes.
func policyRefKindFor(kind string) string { return kind }

// buildRemoteURL converts a RemoteURLInput already checked by validateCreateContent into the
// configv1beta1.RemoteURL PolicyRefs consume.
func buildRemoteURL(input *RemoteURLInput) (*configv1beta1.RemoteURL, error) {
	remoteURL := &configv1beta1.RemoteURL{
		URL:                   input.URL,
		Template:              input.Template,
		InsecureSkipTLSVerify: input.InsecureSkipTLSVerify,
		PlainHTTP:             input.PlainHTTP,
	}
	if input.Interval != "" {
		d, err := time.ParseDuration(input.Interval)
		if err != nil {
			return nil, invalidRequestf("remoteURL.interval is not a valid duration: %v", err)
		}
		remoteURL.Interval = &metav1.Duration{Duration: d}
	}
	if input.SecretRef != nil {
		remoteURL.SecretRef = &corev1.SecretReference{
			Namespace: input.SecretRef.Namespace,
			Name:      input.SecretRef.Name,
		}
	}
	return remoteURL, nil
}

// buildProfileObject returns an unsaved ClusterProfile or Profile object for the given identity
// and spec, ready to be passed to writeClient.Create.
func buildProfileObject(id ProfileIdentity, spec *configv1beta1.Spec) client.Object {
	if id.Kind == configv1beta1.ClusterProfileKind {
		return &configv1beta1.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{Name: id.Name},
			Spec:       *spec,
		}
	}
	return &configv1beta1.Profile{
		ObjectMeta: metav1.ObjectMeta{Namespace: id.Namespace, Name: id.Name},
		Spec:       *spec,
	}
}

// getProfileObject fetches the existing ClusterProfile or Profile for id into a fresh object,
// returned as client.Object so callers can Update/Delete it without a type switch.
func getProfileObject(ctx context.Context, c client.Client, id ProfileIdentity) (client.Object, error) {
	if id.Kind == configv1beta1.ClusterProfileKind {
		obj := &configv1beta1.ClusterProfile{}
		if err := c.Get(ctx, types.NamespacedName{Name: id.Name}, obj); err != nil {
			return nil, err
		}
		return obj, nil
	}
	obj := &configv1beta1.Profile{}
	if err := c.Get(ctx, types.NamespacedName{Namespace: id.Namespace, Name: id.Name}, obj); err != nil {
		return nil, err
	}
	return obj, nil
}

// getProfileSpec returns the Spec currently stored on obj (a *ClusterProfile or *Profile, as
// returned by getProfileObject) and, via setSpec, a way to write a new Spec back onto it before
// Update.
func getProfileSpec(obj client.Object) configv1beta1.Spec {
	switch v := obj.(type) {
	case *configv1beta1.ClusterProfile:
		return v.Spec
	case *configv1beta1.Profile:
		return v.Spec
	default:
		return configv1beta1.Spec{}
	}
}

func setProfileSpec(obj client.Object, spec *configv1beta1.Spec) {
	switch v := obj.(type) {
	case *configv1beta1.ClusterProfile:
		v.Spec = *spec
	case *configv1beta1.Profile:
		v.Spec = *spec
	}
}

// profileExists reports whether a ClusterProfile/Profile with this identity already exists.
// Uses this manager's own cached client (already granted get;list;watch on both kinds) rather
// than the caller's impersonated client - this is a read used for a fast-path UX error, not an
// authorization decision, so it does not need to go through the caller's own identity.
func (m *instance) profileExists(ctx context.Context, id ProfileIdentity) (bool, error) {
	_, err := getProfileObject(ctx, m.client, id)
	if err == nil {
		return true, nil
	}
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

// createProfileObject implements POST /profile: validates the request, checks for a name
// collision, then creates the profile via one of the three content flows. writeClient must be
// the caller's own impersonated client (see k8s-utils.go getImpersonatedClient) - every write
// here is subject to that caller's own RBAC, not this manager's ServiceAccount's.
func (m *instance) createProfileObject(ctx context.Context, writeClient client.Client,
	req *CreateProfileRequest) error {

	if err := validateProfileIdentity(req.ProfileIdentity); err != nil {
		return err
	}
	if err := validateCreateContent(req); err != nil {
		return err
	}

	exists, err := m.profileExists(ctx, req.ProfileIdentity)
	if err != nil {
		return err
	}
	if exists {
		return ErrProfileAlreadyExists
	}

	spec := configv1beta1.Spec{
		ClusterSelector: libsveltosv1beta1.Selector{
			LabelSelector: metav1.LabelSelector{MatchLabels: req.ClusterSelector},
		},
		DependsOn: req.DependsOn,
	}
	if req.Tier != nil {
		spec.Tier = *req.Tier
	}

	switch {
	case req.HelmChart != nil:
		spec.HelmCharts = []configv1beta1.HelmChart{
			{
				RepositoryURL:    req.HelmChart.RepositoryURL,
				RepositoryName:   req.HelmChart.RepositoryName,
				ChartName:        req.HelmChart.ChartName,
				ChartVersion:     req.HelmChart.ChartVersion,
				ReleaseNamespace: req.HelmChart.ReleaseNamespace,
				ReleaseName:      req.HelmChart.ReleaseName,
				Values:           req.HelmChart.Values,
				HelmChartAction:  configv1beta1.HelmChartActionInstall,
			},
		}
		return createProfileWithSpec(ctx, writeClient, req.ProfileIdentity, &spec)

	case req.ExistingContent != nil:
		spec.PolicyRefs = []configv1beta1.PolicyRef{
			{
				Kind:      policyRefKindFor(req.ExistingContent.Kind),
				Name:      req.ExistingContent.Name,
				Namespace: req.ExistingContent.Namespace,
			},
		}
		return createProfileWithSpec(ctx, writeClient, req.ProfileIdentity, &spec)

	case req.RemoteURL != nil:
		remoteURL, err := buildRemoteURL(req.RemoteURL)
		if err != nil {
			return err
		}
		spec.PolicyRefs = []configv1beta1.PolicyRef{{RemoteURL: remoteURL}}
		return createProfileWithSpec(ctx, writeClient, req.ProfileIdentity, &spec)

	default: // req.YAML != nil, enforced by validateCreateContent
		return m.createProfileWithYAML(ctx, writeClient, req, &spec)
	}
}

func createProfileWithSpec(ctx context.Context, writeClient client.Client,
	id ProfileIdentity, spec *configv1beta1.Spec) error {

	obj := buildProfileObject(id, spec)
	if err := writeClient.Create(ctx, obj); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return ErrProfileAlreadyExists
		}
		return err
	}
	return nil
}

// createProfileWithYAML creates the ConfigMap holding the pasted YAML, then the profile
// referencing it. If the profile create fails for any reason - including losing the
// existence-check race - the ConfigMap is deleted so a lost race never orphans one.
func (m *instance) createProfileWithYAML(ctx context.Context, writeClient client.Client,
	req *CreateProfileRequest, spec *configv1beta1.Spec) error {

	cmNamespace := contentConfigMapNamespace(req.ProfileIdentity)
	cmName := contentConfigMapName(req.Name)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: cmNamespace,
		},
		Data: map[string]string{
			contentDataKey: *req.YAML,
		},
	}
	if err := writeClient.Create(ctx, configMap); err != nil {
		return fmt.Errorf("failed to create ConfigMap %s/%s: %w", cmNamespace, cmName, err)
	}

	// Profile: PolicyRef.Namespace must be left empty, it implicitly resolves to the Profile's
	// own namespace. ClusterProfile: must be set explicitly, an empty namespace resolves
	// per-matching-cluster which is not what a single dashboard-created ConfigMap wants.
	policyRefNamespace := ""
	if req.Kind == configv1beta1.ClusterProfileKind {
		policyRefNamespace = cmNamespace
	}
	spec.PolicyRefs = []configv1beta1.PolicyRef{
		{
			Kind:      string(libsveltosv1beta1.ConfigMapReferencedResourceKind),
			Name:      cmName,
			Namespace: policyRefNamespace,
		},
	}

	obj := buildProfileObject(req.ProfileIdentity, spec)
	if err := writeClient.Create(ctx, obj); err != nil {
		if delErr := writeClient.Delete(ctx, configMap); delErr != nil && !apierrors.IsNotFound(delErr) {
			m.logger.V(1).Info(fmt.Sprintf(
				"failed to roll back ConfigMap %s/%s after profile create failed: %v", cmNamespace, cmName, delErr))
		}
		if apierrors.IsAlreadyExists(err) {
			return ErrProfileAlreadyExists
		}
		return err
	}
	return nil
}

// updateProfileObject implements PUT /profile: replaces the object's Spec with the parsed
// SpecYAML, then applies any referenced-content edits. writeClient must be the caller's own
// impersonated client.
func (m *instance) updateProfileObject(ctx context.Context, writeClient client.Client,
	req *UpdateProfileRequest) error {

	if err := validateProfileIdentity(req.ProfileIdentity); err != nil {
		return err
	}

	var spec configv1beta1.Spec
	if err := yaml.Unmarshal([]byte(req.SpecYAML), &spec); err != nil {
		return invalidRequestf("specYAML does not parse as valid YAML: %v", err)
	}

	obj, err := getProfileObject(ctx, writeClient, req.ProfileIdentity)
	if err != nil {
		return err
	}
	setProfileSpec(obj, &spec)
	if err := writeClient.Update(ctx, obj); err != nil {
		return err
	}

	if len(req.ReferencedContent) == 0 {
		return nil
	}
	return updateReferencedContent(ctx, writeClient, req.ProfileIdentity, spec.PolicyRefs, req.ReferencedContent)
}

// updateReferencedContent writes each entry in edits into the data of the ConfigMap/Secret in
// policyRefs whose name matches. Entries in edits with no matching PolicyRef are ignored -
// the edited Spec, not the request map, is authoritative for what this profile now references.
func updateReferencedContent(ctx context.Context, writeClient client.Client, id ProfileIdentity,
	policyRefs []configv1beta1.PolicyRef, edits map[string]string) error {

	for i := range policyRefs {
		ref := &policyRefs[i]
		data, ok := edits[ref.Name]
		if !ok {
			continue
		}
		if ref.Kind != string(libsveltosv1beta1.ConfigMapReferencedResourceKind) &&
			ref.Kind != string(libsveltosv1beta1.SecretReferencedResourceKind) {

			continue
		}

		namespace := ref.Namespace
		if namespace == "" {
			if id.Kind != configv1beta1.ProfileKind {
				// ClusterProfile with an empty PolicyRef.Namespace resolves per-matching-cluster
				// at deploy time - there is no single namespace to edit here. Skip rather than guess.
				continue
			}
			namespace = id.Namespace
		}

		var obj client.Object
		if ref.Kind == string(libsveltosv1beta1.ConfigMapReferencedResourceKind) {
			cm := &corev1.ConfigMap{}
			if err := writeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, cm); err != nil {
				return err
			}
			if cm.Data == nil {
				cm.Data = map[string]string{}
			}
			cm.Data[contentDataKey] = data
			obj = cm
		} else {
			s := &corev1.Secret{}
			if err := writeClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, s); err != nil {
				return err
			}
			if s.StringData == nil {
				s.StringData = map[string]string{}
			}
			s.StringData[contentDataKey] = data
			obj = s
		}
		if err := writeClient.Update(ctx, obj); err != nil {
			return err
		}
	}

	return nil
}

// deleteProfileObject implements DELETE /profile: reads the profile's PolicyRefs (needed to
// know what else to touch before it's gone), deletes the profile, then - only if
// RemoveReferencedContent is set - deletes every ConfigMap/Secret its PolicyRefs pointed at, no
// further check. See ENHANCEMENTS/profile-creation-dashboard.md for why this is a plain,
// user-driven flag rather than automated reference tracking. writeClient must be the caller's
// own impersonated client.
func (m *instance) deleteProfileObject(ctx context.Context, writeClient client.Client,
	req *DeleteProfileRequest) error {

	if err := validateProfileIdentity(req.ProfileIdentity); err != nil {
		return err
	}

	obj, err := getProfileObject(ctx, writeClient, req.ProfileIdentity)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	policyRefs := getProfileSpec(obj).PolicyRefs

	if err := writeClient.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	if !req.RemoveReferencedContent {
		return nil
	}

	for i := range policyRefs {
		ref := &policyRefs[i]
		if ref.Kind != string(libsveltosv1beta1.ConfigMapReferencedResourceKind) &&
			ref.Kind != string(libsveltosv1beta1.SecretReferencedResourceKind) {

			continue
		}

		namespace := ref.Namespace
		if namespace == "" {
			if req.Kind != configv1beta1.ProfileKind {
				// See the matching comment in updateReferencedContent.
				continue
			}
			namespace = req.Namespace
		}

		var content client.Object
		if ref.Kind == string(libsveltosv1beta1.ConfigMapReferencedResourceKind) {
			content = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: ref.Name}}
		} else {
			content = &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: ref.Name}}
		}
		if err := writeClient.Delete(ctx, content); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}

	return nil
}
