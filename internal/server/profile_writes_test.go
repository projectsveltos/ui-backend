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
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2/textlogger"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

const (
	testBogusKind            = "Bogus"
	testRefNamespace         = "ns"
	testRefName              = "n"
	testHelmRepoURL          = "https://charts.example.com"
	testHelmReleaseName      = "r"
	testChartName            = "nginx"
	testDashboardNamespace   = "projectsveltos"
	testSharedNamespace      = "shared"
	testSharedConfigMapName  = "shared-config"
	testContentDataKey       = "content.yaml"
	testTakenProfileName     = "taken"
	testMinimalConfigMapYAML = "kind: ConfigMap"
)

var _ = Describe("Profile writes: validation", func() {
	It("validateProfileIdentity rejects an unsupported Kind", func() {
		err := server.ValidateProfileIdentity(server.ProfileIdentity{Kind: testBogusKind, Name: randomString()})
		Expect(err).ToNot(BeNil())
	})

	It("validateProfileIdentity requires a Name", func() {
		err := server.ValidateProfileIdentity(server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind})
		Expect(err).ToNot(BeNil())
	})

	It("validateProfileIdentity requires a Namespace for Profile but not for ClusterProfile", func() {
		err := server.ValidateProfileIdentity(server.ProfileIdentity{Kind: configv1beta1.ProfileKind, Name: randomString()})
		Expect(err).ToNot(BeNil())

		err = server.ValidateProfileIdentity(server.ProfileIdentity{
			Kind: configv1beta1.ProfileKind, Namespace: randomString(), Name: randomString(),
		})
		Expect(err).To(BeNil())

		err = server.ValidateProfileIdentity(server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: randomString()})
		Expect(err).To(BeNil())
	})

	It("isRemoteHelmSource recognizes http(s):// and oci:// only", func() {
		Expect(server.IsRemoteHelmSource(testHelmRepoURL)).To(BeTrue())
		Expect(server.IsRemoteHelmSource("http://charts.example.com")).To(BeTrue())
		Expect(server.IsRemoteHelmSource("oci://registry.example.com/charts")).To(BeTrue())
		Expect(server.IsRemoteHelmSource("gitrepository://flux-system/flux-system/charts/x")).To(BeFalse())
	})

	It("validateCreateContent rejects zero or more than one content flow", func() {
		err := server.ValidateCreateContent(&server.CreateProfileRequest{})
		Expect(err).ToNot(BeNil())

		yamlContent := "apiVersion: v1\nkind: ConfigMap"
		err = server.ValidateCreateContent(&server.CreateProfileRequest{
			YAML: &yamlContent,
			ExistingContent: &server.ExistingContentRef{
				Kind: string(libsveltosv1beta1.ConfigMapReferencedResourceKind), Namespace: testRefNamespace, Name: testRefName,
			},
		})
		Expect(err).ToNot(BeNil())
	})

	It("validateCreateContent requires repositoryName for an http(s)/oci helm source but not for a Flux one", func() {
		err := server.ValidateCreateContent(&server.CreateProfileRequest{
			HelmChart: &server.HelmChartInput{
				RepositoryURL: testHelmRepoURL, ReleaseName: testHelmReleaseName, ReleaseNamespace: testRefNamespace,
			},
		})
		Expect(err).ToNot(BeNil())

		err = server.ValidateCreateContent(&server.CreateProfileRequest{
			HelmChart: &server.HelmChartInput{
				RepositoryURL: testHelmRepoURL, RepositoryName: "repo",
				ReleaseName: testHelmReleaseName, ReleaseNamespace: testRefNamespace,
			},
		})
		Expect(err).To(BeNil())

		err = server.ValidateCreateContent(&server.CreateProfileRequest{
			HelmChart: &server.HelmChartInput{
				RepositoryURL: "gitrepository://flux-system/flux-system/charts/x",
				ReleaseName:   testHelmReleaseName, ReleaseNamespace: testRefNamespace,
			},
		})
		Expect(err).To(BeNil())
	})

	It("validateCreateContent rejects an existingContent kind other than ConfigMap/Secret", func() {
		err := server.ValidateCreateContent(&server.CreateProfileRequest{
			ExistingContent: &server.ExistingContentRef{Kind: testBogusKind, Namespace: testRefNamespace, Name: testRefName},
		})
		Expect(err).ToNot(BeNil())

		err = server.ValidateCreateContent(&server.CreateProfileRequest{
			ExistingContent: &server.ExistingContentRef{
				Kind: string(libsveltosv1beta1.SecretReferencedResourceKind), Namespace: testRefNamespace, Name: testRefName,
			},
		})
		Expect(err).To(BeNil())
	})

	It("contentConfigMapNamespace uses the Profile's own namespace, and the fixed namespace for ClusterProfile", func() {
		ns := randomString()
		Expect(server.ContentConfigMapNamespace(server.ProfileIdentity{
			Kind: configv1beta1.ProfileKind, Namespace: ns, Name: randomString(),
		})).To(Equal(ns))
		Expect(server.ContentConfigMapNamespace(server.ProfileIdentity{
			Kind: configv1beta1.ClusterProfileKind, Name: randomString(),
		})).To(Equal(testDashboardNamespace))
	})

	It("contentConfigMapName appends -content for a normal name", func() {
		Expect(server.ContentConfigMapName("my-profile")).To(Equal("my-profile-content"))
	})

	It("contentConfigMapName truncates and hashes a name that would exceed 253 characters", func() {
		longName := strings.Repeat("a", 260)
		name := server.ContentConfigMapName(longName)
		Expect(len(name)).To(BeNumerically("<=", 253))
		Expect(name).To(HavePrefix(strings.Repeat("a", 10)))

		// Two names that only differ after the truncation point must not collide.
		otherLongName := strings.Repeat("a", 259) + "b"
		otherName := server.ContentConfigMapName(otherLongName)
		Expect(otherName).ToNot(Equal(name))
	})
})

var _ = Describe("Profile writes: create", func() {
	var logger = textlogger.NewLogger(textlogger.NewConfig())

	It("creates a ClusterProfile from the Helm flow", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		m := server.NewTestInstance(c, logger)

		req := &server.CreateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: randomString()},
			ClusterSelector: map[string]string{testEnvLabelKey: testEnvValue},
			HelmChart: &server.HelmChartInput{
				RepositoryURL: "oci://registry.example.com/charts", RepositoryName: "charts", ChartName: testChartName,
				ReleaseName: testChartName, ReleaseNamespace: "nginx-system",
			},
		}
		Expect(m.CreateProfileObject(context.TODO(), c, req)).To(Succeed())

		cp := &configv1beta1.ClusterProfile{}
		Expect(c.Get(context.TODO(), types.NamespacedName{Name: req.Name}, cp)).To(Succeed())
		Expect(cp.Spec.HelmCharts).To(HaveLen(1))
		Expect(cp.Spec.HelmCharts[0].ReleaseName).To(Equal(testChartName))
		Expect(cp.Spec.HelmCharts[0].HelmChartAction).To(Equal(configv1beta1.HelmChartActionInstall))
		Expect(cp.Spec.ClusterSelector.MatchLabels).To(Equal(map[string]string{testEnvLabelKey: testEnvValue}))
	})

	It("creates a Profile and its ConfigMap from the YAML flow, PolicyRef.Namespace left empty", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		m := server.NewTestInstance(c, logger)

		ns := randomString()
		name := randomString()
		yamlContent := "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app-config\n"
		req := &server.CreateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ProfileKind, Namespace: ns, Name: name},
			ClusterSelector: map[string]string{testEnvLabelKey: testEnvValue},
			YAML:            &yamlContent,
		}
		Expect(m.CreateProfileObject(context.TODO(), c, req)).To(Succeed())

		p := &configv1beta1.Profile{}
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: name}, p)).To(Succeed())
		Expect(p.Spec.PolicyRefs).To(HaveLen(1))
		Expect(p.Spec.PolicyRefs[0].Kind).To(Equal(string(libsveltosv1beta1.ConfigMapReferencedResourceKind)))
		Expect(p.Spec.PolicyRefs[0].Namespace).To(BeEmpty(), "Profile PolicyRef.Namespace must be left empty")

		cm := &corev1.ConfigMap{}
		Expect(c.Get(context.TODO(),
			types.NamespacedName{Namespace: ns, Name: p.Spec.PolicyRefs[0].Name}, cm)).To(Succeed())
		Expect(cm.Data[testContentDataKey]).To(Equal(yamlContent))
	})

	It("creates a ClusterProfile's ConfigMap in the fixed dashboard namespace, PolicyRef.Namespace set explicitly", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		m := server.NewTestInstance(c, logger)

		name := randomString()
		yamlContent := "apiVersion: v1\nkind: ConfigMap\n"
		req := &server.CreateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: name},
			ClusterSelector: map[string]string{testEnvLabelKey: testEnvValue},
			YAML:            &yamlContent,
		}
		Expect(m.CreateProfileObject(context.TODO(), c, req)).To(Succeed())

		cp := &configv1beta1.ClusterProfile{}
		Expect(c.Get(context.TODO(), types.NamespacedName{Name: name}, cp)).To(Succeed())
		Expect(cp.Spec.PolicyRefs).To(HaveLen(1))
		Expect(cp.Spec.PolicyRefs[0].Namespace).To(Equal(testDashboardNamespace))

		cm := &corev1.ConfigMap{}
		Expect(c.Get(context.TODO(),
			types.NamespacedName{Namespace: testDashboardNamespace, Name: cp.Spec.PolicyRefs[0].Name}, cm)).To(Succeed())
	})

	It("creates a profile referencing an existing ConfigMap without creating a new one (ExistingContent flow)", func() {
		existingCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: testSharedNamespace, Name: testSharedConfigMapName},
			Data:       map[string]string{testContentDataKey: "kind: Deployment"},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingCM).Build()
		m := server.NewTestInstance(c, logger)

		name := randomString()
		req := &server.CreateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: name},
			ClusterSelector: map[string]string{testEnvLabelKey: testEnvValue},
			ExistingContent: &server.ExistingContentRef{
				Kind:      string(libsveltosv1beta1.ConfigMapReferencedResourceKind),
				Namespace: testSharedNamespace, Name: testSharedConfigMapName,
			},
		}
		Expect(m.CreateProfileObject(context.TODO(), c, req)).To(Succeed())

		cp := &configv1beta1.ClusterProfile{}
		Expect(c.Get(context.TODO(), types.NamespacedName{Name: name}, cp)).To(Succeed())
		Expect(cp.Spec.PolicyRefs).To(ConsistOf(configv1beta1.PolicyRef{
			Kind:      string(libsveltosv1beta1.ConfigMapReferencedResourceKind),
			Namespace: testSharedNamespace, Name: testSharedConfigMapName,
		}))

		// Only the pre-existing ConfigMap exists - no second one was created for this profile.
		cmList := &corev1.ConfigMapList{}
		Expect(c.List(context.TODO(), cmList)).To(Succeed())
		Expect(cmList.Items).To(HaveLen(1))
	})

	It("rejects create when a profile with the same identity already exists", func() {
		existing := &configv1beta1.ClusterProfile{ObjectMeta: metav1.ObjectMeta{Name: testTakenProfileName}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
		m := server.NewTestInstance(c, logger)

		yamlContent := testMinimalConfigMapYAML
		req := &server.CreateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: testTakenProfileName},
			ClusterSelector: map[string]string{testEnvLabelKey: testEnvValue},
			YAML:            &yamlContent,
		}
		err := m.CreateProfileObject(context.TODO(), c, req)
		Expect(err).ToNot(BeNil())
		Expect(errors.Is(err, server.ErrProfileAlreadyExists)).To(BeTrue())

		// No ConfigMap should have been created either - the existence check runs first.
		cmList := &corev1.ConfigMapList{}
		Expect(c.List(context.TODO(), cmList)).To(Succeed())
		Expect(cmList.Items).To(BeEmpty())
	})

	It("rolls back the ConfigMap if the profile Create call fails after it (lost race)", func() {
		name := randomString()
		raced := false
		c := fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
			Create: func(ctx context.Context, cl client.WithWatch, obj client.Object,
				opts ...client.CreateOption) error {
				if _, ok := obj.(*configv1beta1.ClusterProfile); ok && !raced {
					raced = true
					return apierrors.NewAlreadyExists(
						schema.GroupResource{Group: configv1beta1.GroupVersion.Group, Resource: "clusterprofiles"},
						obj.GetName())
				}
				return cl.Create(ctx, obj, opts...)
			},
		}).Build()
		m := server.NewTestInstance(c, logger)

		yamlContent := testMinimalConfigMapYAML
		req := &server.CreateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: name},
			ClusterSelector: map[string]string{testEnvLabelKey: testEnvValue},
			YAML:            &yamlContent,
		}
		err := m.CreateProfileObject(context.TODO(), c, req)
		Expect(err).ToNot(BeNil())
		Expect(errors.Is(err, server.ErrProfileAlreadyExists)).To(BeTrue())

		// The ConfigMap created before the raced profile-create failure must have been rolled back.
		cmList := &corev1.ConfigMapList{}
		Expect(c.List(context.TODO(), cmList)).To(Succeed())
		Expect(cmList.Items).To(BeEmpty())
	})
})

var _ = Describe("Profile writes: update", func() {
	var logger = textlogger.NewLogger(textlogger.NewConfig())

	It("replaces the profile's Spec with the parsed SpecYAML", func() {
		name := randomString()
		existing := &configv1beta1.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec:       configv1beta1.Spec{Tier: 100},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
		m := server.NewTestInstance(c, logger)

		req := &server.UpdateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: name},
			SpecYAML:        "tier: 200\nsyncMode: Continuous\n",
		}
		Expect(m.UpdateProfileObject(context.TODO(), c, req)).To(Succeed())

		cp := &configv1beta1.ClusterProfile{}
		Expect(c.Get(context.TODO(), types.NamespacedName{Name: name}, cp)).To(Succeed())
		Expect(cp.Spec.Tier).To(Equal(int32(200)))
		Expect(cp.Spec.SyncMode).To(Equal(configv1beta1.SyncMode("Continuous")))
	})

	It("rejects an update whose SpecYAML does not parse", func() {
		name := randomString()
		existing := &configv1beta1.ClusterProfile{ObjectMeta: metav1.ObjectMeta{Name: name}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
		m := server.NewTestInstance(c, logger)

		req := &server.UpdateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: name},
			SpecYAML:        "tier: [this is not valid: yaml",
		}
		err := m.UpdateProfileObject(context.TODO(), c, req)
		Expect(err).ToNot(BeNil())
		Expect(errors.Is(err, server.ErrInvalidRequest)).To(BeTrue())
	})

	It("updates the data of a referenced ConfigMap named in ReferencedContent", func() {
		ns := randomString()
		name := randomString()
		cmName := name + "-content"
		existing := &configv1beta1.Profile{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
			Spec: configv1beta1.Spec{
				PolicyRefs: []configv1beta1.PolicyRef{
					{Kind: string(libsveltosv1beta1.ConfigMapReferencedResourceKind), Name: cmName},
				},
			},
		}
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: cmName},
			Data:       map[string]string{testContentDataKey: "old"},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing, cm).Build()
		m := server.NewTestInstance(c, logger)

		req := &server.UpdateProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ProfileKind, Namespace: ns, Name: name},
			SpecYAML:        "policyRefs:\n- kind: ConfigMap\n  name: " + cmName + "\n",
			ReferencedContent: map[string]string{
				cmName: "new content",
			},
		}
		Expect(m.UpdateProfileObject(context.TODO(), c, req)).To(Succeed())

		updated := &corev1.ConfigMap{}
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: cmName}, updated)).To(Succeed())
		Expect(updated.Data[testContentDataKey]).To(Equal("new content"))
	})

	It("ignores a ReferencedContent entry for a name not in the edited Spec's PolicyRefs", func() {
		ns := randomString()
		name := randomString()
		existing := &configv1beta1.Profile{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()
		m := server.NewTestInstance(c, logger)

		req := &server.UpdateProfileRequest{
			ProfileIdentity:   server.ProfileIdentity{Kind: configv1beta1.ProfileKind, Namespace: ns, Name: name},
			SpecYAML:          "tier: 100\n",
			ReferencedContent: map[string]string{"not-referenced": "content"},
		}
		// Must not error just because the ConfigMap named in ReferencedContent doesn't exist -
		// it isn't in the edited Spec's PolicyRefs, so it's ignored entirely.
		Expect(m.UpdateProfileObject(context.TODO(), c, req)).To(Succeed())
	})
})

var _ = Describe("Profile writes: delete", func() {
	var logger = textlogger.NewLogger(textlogger.NewConfig())

	It("deletes only the profile when RemoveReferencedContent is false", func() {
		ns := randomString()
		name := randomString()
		cmName := name + "-content"
		existing := &configv1beta1.Profile{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
			Spec: configv1beta1.Spec{
				PolicyRefs: []configv1beta1.PolicyRef{
					{Kind: string(libsveltosv1beta1.ConfigMapReferencedResourceKind), Name: cmName},
				},
			},
		}
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: cmName}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing, cm).Build()
		m := server.NewTestInstance(c, logger)

		req := &server.DeleteProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ProfileKind, Namespace: ns, Name: name},
		}
		Expect(m.DeleteProfileObject(context.TODO(), c, req)).To(Succeed())

		p := &configv1beta1.Profile{}
		err := c.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: name}, p)
		Expect(err).ToNot(BeNil())

		// ConfigMap untouched.
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: cmName}, cm)).To(Succeed())
	})

	It("deletes the profile and its referenced ConfigMap when RemoveReferencedContent is true", func() {
		ns := randomString()
		name := randomString()
		cmName := name + "-content"
		existing := &configv1beta1.Profile{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
			Spec: configv1beta1.Spec{
				PolicyRefs: []configv1beta1.PolicyRef{
					{Kind: string(libsveltosv1beta1.ConfigMapReferencedResourceKind), Name: cmName},
				},
			},
		}
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: cmName}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing, cm).Build()
		m := server.NewTestInstance(c, logger)

		req := &server.DeleteProfileRequest{
			ProfileIdentity:         server.ProfileIdentity{Kind: configv1beta1.ProfileKind, Namespace: ns, Name: name},
			RemoveReferencedContent: true,
		}
		Expect(m.DeleteProfileObject(context.TODO(), c, req)).To(Succeed())

		err := c.Get(context.TODO(), types.NamespacedName{Namespace: ns, Name: cmName}, cm)
		Expect(err).ToNot(BeNil())
	})

	It("does not delete a ClusterProfile's referenced content when the PolicyRef.Namespace is empty (ambiguous)", func() {
		name := randomString()
		cmName := name + "-content"
		existing := &configv1beta1.ClusterProfile{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: configv1beta1.Spec{
				PolicyRefs: []configv1beta1.PolicyRef{
					// Namespace intentionally empty: for ClusterProfile this resolves
					// per-matching-cluster, not to any single namespace we could delete from.
					{Kind: string(libsveltosv1beta1.ConfigMapReferencedResourceKind), Name: cmName},
				},
			},
		}
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: testDashboardNamespace, Name: cmName}}
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing, cm).Build()
		m := server.NewTestInstance(c, logger)

		req := &server.DeleteProfileRequest{
			ProfileIdentity:         server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: name},
			RemoveReferencedContent: true,
		}
		Expect(m.DeleteProfileObject(context.TODO(), c, req)).To(Succeed())

		// Left alone - we could not safely determine which namespace it lives in.
		Expect(c.Get(context.TODO(), types.NamespacedName{Namespace: testDashboardNamespace, Name: cmName}, cm)).To(Succeed())
	})

	It("is a no-op, not an error, when the profile does not exist", func() {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		m := server.NewTestInstance(c, logger)

		req := &server.DeleteProfileRequest{
			ProfileIdentity: server.ProfileIdentity{Kind: configv1beta1.ClusterProfileKind, Name: randomString()},
		}
		Expect(m.DeleteProfileObject(context.TODO(), c, req)).To(Succeed())
	})
})
