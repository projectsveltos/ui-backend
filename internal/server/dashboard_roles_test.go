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

	authorizationv1 "k8s.io/api/authorization/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

const (
	userRBACDir       = "../../config/user-rbac"
	dashboardViewer   = "sveltos-dashboard-viewer"
	dashboardEditor   = "sveltos-dashboard-editor"
	viewerRoleFile    = "dashboard_viewer.yaml"
	editorRoleFile    = "dashboard_editor.yaml"
	secretsResource   = "secrets"
	configMapResource = "configmaps"

	rbacSveltosClusters              = "sveltosclusters"
	rbacClusters                     = "clusters"
	rbacClusterProfiles              = "clusterprofiles"
	rbacProfiles                     = "profiles"
	rbacClusterSummaries             = "clustersummaries"
	rbacEventTriggers                = "eventtriggers"
	rbacClassifiers                  = "classifiers"
	rbacManagementClusterClassifiers = "managementclusterclassifiers"

	verbGet    = "get"
	verbList   = "list"
	verbCreate = "create"
	verbUpdate = "update"
	verbPatch  = "patch"
	verbDelete = "delete"
)

// These tests load the shipped dashboard ClusterRoles from config/user-rbac and evaluate them with
// real SubjectAccessReviews, so the roles cannot drift from what ui-backend checks and sends.
var _ = Describe("Dashboard ClusterRoles", func() {
	var k8sClient client.Client

	BeforeEach(func() {
		var err error
		k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
		Expect(err).To(BeNil())
	})

	// createShippedRole creates the ClusterRole defined in config/user-rbac/<fileName> under a
	// random name (the shipped name may already exist from another spec) and returns that name.
	createShippedRole := func(fileName, expectedName string) string {
		content, err := os.ReadFile(filepath.Join(userRBACDir, fileName))
		Expect(err).To(BeNil())

		role := &rbacv1.ClusterRole{}
		Expect(yaml.Unmarshal(content, role)).To(Succeed())
		Expect(role.Name).To(Equal(expectedName))

		role.Name = randomString()
		Expect(k8sClient.Create(context.TODO(), role)).To(Succeed())
		return role.Name
	}

	bindToUser := func(user, roleName string) {
		binding := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: randomString()},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: clusterRoleKind, Name: roleName},
			Subjects:   []rbacv1.Subject{{Kind: rbacv1.UserKind, Name: user, APIGroup: rbacv1.GroupName}},
		}
		Expect(k8sClient.Create(context.TODO(), binding)).To(Succeed())
	}

	isAllowed := func(user, verb, group, resource string) bool {
		sar := &authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				User: user,
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Verb:     verb,
					Group:    group,
					Resource: resource,
				},
			},
		}
		Expect(k8sClient.Create(context.TODO(), sar)).To(Succeed())
		return sar.Status.Allowed
	}

	// allDashboardReadChecks returns, by name, every list check ui-backend runs for a user.
	allDashboardReadChecks := func(user string) map[string]func() (bool, error) {
		m := server.NewTestInstanceWithConfig(cfg, k8sClient, textlogger.NewLogger(textlogger.NewConfig()))
		return map[string]func() (bool, error){
			rbacSveltosClusters:              func() (bool, error) { return m.CanListSveltosClusters(user, nil) },
			rbacClusters:                     func() (bool, error) { return m.CanListCAPIClusters(user, nil) },
			rbacClusterProfiles:              func() (bool, error) { return m.CanListClusterProfiles(user, nil) },
			rbacProfiles:                     func() (bool, error) { return m.CanListProfiles(user, nil) },
			rbacClusterSummaries:             func() (bool, error) { return m.CanListClusterSummaries(user, nil) },
			rbacEventTriggers:                func() (bool, error) { return m.CanListEventTriggers(user, nil) },
			rbacClassifiers:                  func() (bool, error) { return m.CanListClassifiers(user, nil) },
			rbacManagementClusterClassifiers: func() (bool, error) { return m.CanListManagementClusterClassifiers(user, nil) },
		}
	}

	expectCanRead := func(user string) {
		for name, check := range allDashboardReadChecks(user) {
			// The apiserver's RBAC authorizer may briefly lag behind a just-created binding.
			Eventually(check).Should(BeTrue(), "list %s", name)
		}
	}

	writableGroup := configv1beta1.GroupVersion.Group
	writableResources := []string{rbacClusterProfiles, rbacProfiles}
	writeVerbs := []string{verbCreate, verbUpdate, verbPatch, verbDelete}

	It("sveltos-dashboard-viewer allows every dashboard read and no write", func() {
		user := randomString()
		bindToUser(user, createShippedRole(viewerRoleFile, dashboardViewer))

		expectCanRead(user)

		for _, resource := range writableResources {
			for _, verb := range writeVerbs {
				Expect(isAllowed(user, verb, writableGroup, resource)).To(BeFalse(), "%s %s", verb, resource)
			}
		}
	})

	It("sveltos-dashboard-editor allows every dashboard read and writes only to ClusterProfiles and Profiles", func() {
		user := randomString()
		bindToUser(user, createShippedRole(editorRoleFile, dashboardEditor))

		expectCanRead(user)

		for _, resource := range writableResources {
			for _, verb := range writeVerbs {
				Expect(isAllowed(user, verb, writableGroup, resource)).To(BeTrue(), "%s %s", verb, resource)
			}
		}

		Expect(isAllowed(user, verbCreate, libsveltosv1beta1.GroupVersion.Group, rbacSveltosClusters)).To(BeFalse())
		Expect(isAllowed(user, verbUpdate, libsveltosv1beta1.GroupVersion.Group, rbacEventTriggers)).To(BeFalse())
		Expect(isAllowed(user, verbUpdate, writableGroup, rbacClusterSummaries)).To(BeFalse())
	})

	It("neither role grants access to Secrets or ConfigMaps", func() {
		viewer := randomString()
		bindToUser(viewer, createShippedRole(viewerRoleFile, dashboardViewer))
		editor := randomString()
		bindToUser(editor, createShippedRole(editorRoleFile, dashboardEditor))

		for _, user := range []string{viewer, editor} {
			for _, resource := range []string{secretsResource, configMapResource} {
				for _, verb := range []string{verbGet, verbList, verbCreate, verbUpdate, verbDelete} {
					Expect(isAllowed(user, verb, "", resource)).To(BeFalse(), "%s %s", verb, resource)
				}
			}
		}
	})
})
