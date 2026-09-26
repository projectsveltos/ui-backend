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

package server_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2/textlogger"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	eventv1beta1 "github.com/projectsveltos/event-manager/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

// testCACert is a throwaway self-signed certificate, valid only as PEM fixture data for
// certutil.NewPool in the getKubernetesRestConfig tests below. It signs nothing real.
const testCACert = `-----BEGIN CERTIFICATE-----
MIIBjjCCATWgAwIBAgIUHrshAui6vKaNqub1M4dlmF4fJAswCgYIKoZIzj0EAwIw
HTEbMBkGA1UEAwwSdGVzdC1vaWRjLXByb3h5LWNhMB4XDTI2MDkyNTIwMDcyNFoX
DTM2MDkyMjIwMDcyNFowHTEbMBkGA1UEAwwSdGVzdC1vaWRjLXByb3h5LWNhMFkw
EwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAELArG+XnaTu58ShXg3/u/RkCjsoZuoT11
bUPUHl3sztMwohF9W4ZCarIbyTMx5rhPJopsmpwIHQXmzKv6TiK9fKNTMFEwHQYD
VR0OBBYEFLCInWZJURTDKIsdE3yabV6Ws3dDMB8GA1UdIwQYMBaAFLCInWZJURTD
KIsdE3yabV6Ws3dDMA8GA1UdEwEB/wQFMAMBAf8wCgYIKoZIzj0EAwIDRwAwRAIg
PWQ6UUeQgXtEfDmPYE0jYGqTC6eHyC2lQe3tRVVJMb0CIDdohBiPaw8X/0+GfUUO
KPNrrouhZT4docpWLAsy2pVh
-----END CERTIFICATE-----
`

// These tests exercise the canList*/canGet* SubjectAccessReview checks against a real,
// RBAC-enabled envtest apiserver (see cfg/testEnv in suite_test.go). A fake client.Client
// cannot evaluate RBAC, so a bug in the SubjectAccessReview.Resource field (e.g. sending the
// Kind, such as "SveltosCluster", instead of the plural REST resource name, such as
// "sveltosclusters") would never match a real ClusterRole rule and every check would silently
// deny non-wildcard roles. Each test below grants a scoped, non-wildcard ClusterRole and
// expects the corresponding canList* call to allow it.
const clusterRoleKind = "ClusterRole"

var _ = Describe("SubjectAccessReview authorization", func() {
	var k8sClient client.Client
	var logger = textlogger.NewLogger(textlogger.NewConfig())

	BeforeEach(func() {
		var err error
		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
		Expect(err).To(BeNil())
	})

	// bindClusterRole grants verb on resource (in group) to user via a cluster-scoped
	// ClusterRole/ClusterRoleBinding pair.
	bindClusterRole := func(user, group, resource, verb string) {
		role := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: randomString()},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{group}, Resources: []string{resource}, Verbs: []string{verb}},
			},
		}
		Expect(k8sClient.Create(context.TODO(), role)).To(Succeed())

		binding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: randomString()},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     clusterRoleKind,
				Name:     role.Name,
			},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.UserKind, Name: user, APIGroup: rbacv1.GroupName},
			},
		}
		Expect(k8sClient.Create(context.TODO(), binding)).To(Succeed())
	}

	// bindClusterRoleToGroup is like bindClusterRole but grants the ClusterRole to a Group
	// subject instead of a User subject. This is the shape RBAC takes for OIDC/Entra ID
	// group-based access (e.g. an AKS ClusterRoleBinding to an Azure AD group).
	bindClusterRoleToGroup := func(group, resourceGroup, resource, verb string) {
		role := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{Name: randomString()},
			Rules: []rbacv1.PolicyRule{
				{APIGroups: []string{resourceGroup}, Resources: []string{resource}, Verbs: []string{verb}},
			},
		}
		Expect(k8sClient.Create(context.TODO(), role)).To(Succeed())

		binding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: randomString()},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     clusterRoleKind,
				Name:     role.Name,
			},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.GroupKind, Name: group, APIGroup: rbacv1.GroupName},
			},
		}
		Expect(k8sClient.Create(context.TODO(), binding)).To(Succeed())
	}

	It("canListSveltosClusters denies a user whose only access comes from a Group binding "+
		"when the caller's groups are not passed in", func() {
		// Regression test: getUserFromToken/validateToken must propagate the caller's group
		// memberships into every SubjectAccessReview, not just the username. Per the
		// SubjectAccessReviewSpec.User doc, specifying User without Groups is interpreted as
		// "what if User were not a member of any groups" - so a real RBAC ClusterRoleBinding
		// to a Group subject (the common OIDC/Entra ID shape) is invisible if groups is nil,
		// even though the same binding grants access via kubectl --as-group.
		user := randomString()
		group := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		bindClusterRoleToGroup(group, libsveltosv1beta1.GroupVersion.Group, "sveltosclusters", "list")

		// The apiserver's RBAC authorizer is backed by an informer cache and may briefly lag
		// behind a just-created ClusterRoleBinding: wait until the binding is visible with
		// groups, then confirm that omitting groups is what denies access.
		Eventually(func() bool {
			allowed, err := m.CanListSveltosClusters(user, []string{group})
			return err == nil && allowed
		}).Should(BeTrue())

		allowed, err := m.CanListSveltosClusters(user, nil)
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())
	})

	It("canListSveltosClusters allows a user whose only access comes from a Group binding "+
		"once the caller's groups are passed in", func() {
		user := randomString()
		group := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		allowed, err := m.CanListSveltosClusters(user, []string{group})
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())

		bindClusterRoleToGroup(group, libsveltosv1beta1.GroupVersion.Group, "sveltosclusters", "list")

		Eventually(func() bool {
			allowed, err := m.CanListSveltosClusters(user, []string{group})
			return err == nil && allowed
		}).Should(BeTrue())
	})

	It("canListSveltosClusters denies by default and allows once bound to sveltosclusters", func() {
		user := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		allowed, err := m.CanListSveltosClusters(user, nil)
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())

		bindClusterRole(user, libsveltosv1beta1.GroupVersion.Group, "sveltosclusters", "list")

		// The apiserver's RBAC authorizer is backed by an informer cache and may briefly lag
		// behind a just-created ClusterRoleBinding.
		Eventually(func() bool {
			allowed, err := m.CanListSveltosClusters(user, nil)
			return err == nil && allowed
		}).Should(BeTrue())
	})

	It("canListCAPIClusters denies by default and allows once bound to clusters", func() {
		user := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		allowed, err := m.CanListCAPIClusters(user, nil)
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())

		bindClusterRole(user, clusterv1.GroupVersion.Group, "clusters", "list")

		Eventually(func() bool {
			allowed, err := m.CanListCAPIClusters(user, nil)
			return err == nil && allowed
		}).Should(BeTrue())
	})

	It("canListClusterProfiles denies by default and allows once bound to clusterprofiles", func() {
		user := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		allowed, err := m.CanListClusterProfiles(user, nil)
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())

		bindClusterRole(user, configv1beta1.GroupVersion.Group, "clusterprofiles", "get")

		Eventually(func() bool {
			allowed, err := m.CanListClusterProfiles(user, nil)
			return err == nil && allowed
		}).Should(BeTrue())
	})

	It("canListProfiles denies by default and allows once bound to profiles", func() {
		user := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		allowed, err := m.CanListProfiles(user, nil)
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())

		bindClusterRole(user, configv1beta1.GroupVersion.Group, "profiles", "get")

		Eventually(func() bool {
			allowed, err := m.CanListProfiles(user, nil)
			return err == nil && allowed
		}).Should(BeTrue())
	})

	It("canListClusterSummaries denies by default and allows once bound to clustersummaries", func() {
		user := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		allowed, err := m.CanListClusterSummaries(user, nil)
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())

		bindClusterRole(user, configv1beta1.GroupVersion.Group, "clustersummaries", "list")

		Eventually(func() bool {
			allowed, err := m.CanListClusterSummaries(user, nil)
			return err == nil && allowed
		}).Should(BeTrue())
	})

	It("canListEventTriggers denies by default and allows once bound to eventtriggers", func() {
		user := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		allowed, err := m.CanListEventTriggers(user, nil)
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())

		bindClusterRole(user, eventv1beta1.GroupVersion.Group, "eventtriggers", "get")

		Eventually(func() bool {
			allowed, err := m.CanListEventTriggers(user, nil)
			return err == nil && allowed
		}).Should(BeTrue())
	})

	It("canListClassifiers denies by default and allows once bound to classifiers", func() {
		user := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		allowed, err := m.CanListClassifiers(user, nil)
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())

		bindClusterRole(user, libsveltosv1beta1.GroupVersion.Group, "classifiers", "get")

		Eventually(func() bool {
			allowed, err := m.CanListClassifiers(user, nil)
			return err == nil && allowed
		}).Should(BeTrue())
	})

	It("canListManagementClusterClassifiers denies by default and allows once bound to managementclusterclassifiers", func() {
		user := randomString()
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		allowed, err := m.CanListManagementClusterClassifiers(user, nil)
		Expect(err).To(BeNil())
		Expect(allowed).To(BeFalse())

		bindClusterRole(user, libsveltosv1beta1.GroupVersion.Group, "managementclusterclassifiers", "get")

		Eventually(func() bool {
			allowed, err := m.CanListManagementClusterClassifiers(user, nil)
			return err == nil && allowed
		}).Should(BeTrue())
	})
})

var _ = Describe("isCAPIInstalled", func() {
	var k8sClient client.Client

	BeforeEach(func() {
		var err error
		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
		Expect(err).To(BeNil())
	})

	It("returns false when the CAPI Cluster CRD is not registered", func() {
		logger := textlogger.NewLogger(textlogger.NewConfig())
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		installed, err := m.IsCAPIInstalled(context.TODO())
		Expect(err).To(BeNil())
		Expect(installed).To(BeFalse())
	})

	It("returns true once the CAPI Cluster CRD is registered", func() {
		crd := &apiextensionsv1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{Name: server.CapiClusterCRDName},
			Spec: apiextensionsv1.CustomResourceDefinitionSpec{
				Group: clusterv1.GroupVersion.Group,
				Names: apiextensionsv1.CustomResourceDefinitionNames{
					Plural:   "clusters",
					Singular: "cluster",
					Kind:     clusterv1.ClusterKind,
					ListKind: "ClusterList",
				},
				Scope: apiextensionsv1.NamespaceScoped,
				Versions: []apiextensionsv1.CustomResourceDefinitionVersion{
					{
						Name:    clusterv1.GroupVersion.Version,
						Served:  true,
						Storage: true,
						Schema: &apiextensionsv1.CustomResourceValidation{
							OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
								Type:                   "object",
								XPreserveUnknownFields: ptr.To(true),
							},
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(context.TODO(), crd)).To(Succeed())

		logger := textlogger.NewLogger(textlogger.NewConfig())
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, logger)

		Eventually(func() bool {
			installed, err := m.IsCAPIInstalled(context.TODO())
			return err == nil && installed
		}).Should(BeTrue())
	})
})

var _ = Describe("getKubernetesRestConfig OIDC proxy routing", func() {
	// The fallback branch (oidcProxyHost unset, falling back to config.Host and the API
	// server's own /var/run/secrets/kubernetes.io/serviceaccount/ca.crt) is pre-existing,
	// unchanged behavior that only resolves inside a real pod, so it's intentionally not
	// covered here. These tests cover only the new OIDC-proxy branch.
	var logger = textlogger.NewLogger(textlogger.NewConfig())
	var apiServerConfig = &rest.Config{Host: "https://kubernetes.default.svc:443"}
	const token = "test-token"
	const proxyHost = "https://kube-oidc-proxy.kube-oidc-proxy.svc.cluster.local:443"

	It("routes to the OIDC proxy and pins its CA when both are set", func() {
		caFile := filepath.Join(GinkgoT().TempDir(), "proxy-ca.pem")
		Expect(os.WriteFile(caFile, []byte(testCACert), 0o600)).To(Succeed())

		m := server.NewTestInstanceWithOIDCProxy(apiServerConfig, proxyHost, caFile, nil, logger)

		restConfig, err := m.GetKubernetesRestConfig(token)

		Expect(err).To(BeNil())
		Expect(restConfig.Host).To(Equal(proxyHost))
		Expect(restConfig.BearerToken).To(Equal(token))
		Expect(restConfig.TLSClientConfig.CAFile).To(Equal(caFile))
	})

	It("routes to the OIDC proxy without pinning a CA when the proxy CA file is left empty", func() {
		m := server.NewTestInstanceWithOIDCProxy(apiServerConfig, proxyHost, "", nil, logger)

		restConfig, err := m.GetKubernetesRestConfig(token)

		Expect(err).To(BeNil())
		Expect(restConfig.Host).To(Equal(proxyHost))
		Expect(restConfig.TLSClientConfig.CAFile).To(BeEmpty())
	})

	It("returns an error when the configured OIDC proxy CA file cannot be read", func() {
		missingCAFile := filepath.Join(GinkgoT().TempDir(), "does-not-exist.pem")
		m := server.NewTestInstanceWithOIDCProxy(apiServerConfig, proxyHost, missingCAFile, nil, logger)

		_, err := m.GetKubernetesRestConfig(token)

		Expect(err).ToNot(BeNil())
	})
})
