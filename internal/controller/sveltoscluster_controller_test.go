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

package controller_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	libsveltosv1alpha1 "github.com/projectsveltos/libsveltos/api/v1alpha1"
	"github.com/projectsveltos/ui-backend/internal/controller"
	"github.com/projectsveltos/ui-backend/internal/server"
)

var _ = Describe("SveltosClusterReconciler", func() {
	var sveltosCluster *libsveltosv1alpha1.SveltosCluster

	BeforeEach(func() {
		sveltosCluster = &libsveltosv1alpha1.SveltosCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      randomString(),
				Namespace: randomString(),
			},
			Spec: libsveltosv1alpha1.SveltosClusterSpec{
				KubeconfigName: randomString(),
			},
		}
	})

	It("reconcile adds/removes SveltosCluster to/from list of existing clusters", func() {
		sveltosCluster.Status = libsveltosv1alpha1.SveltosClusterStatus{
			Ready:   true,
			Version: "v1.29.0",
		}
		initObjects := []client.Object{
			sveltosCluster,
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(initObjects...).
			WithObjects(initObjects...).Build()

		reconciler := getSveltosClusterReconciler(c)

		sveltosClusterName := client.ObjectKey{
			Name:      sveltosCluster.Name,
			Namespace: sveltosCluster.Namespace,
		}

		_, err := reconciler.Reconcile(context.TODO(), ctrl.Request{
			NamespacedName: sveltosClusterName,
		})
		Expect(err).ToNot(HaveOccurred())

		cluster := &corev1.ObjectReference{
			Namespace:  sveltosCluster.Namespace,
			Name:       sveltosCluster.Name,
			Kind:       libsveltosv1alpha1.SveltosClusterKind,
			APIVersion: libsveltosv1alpha1.GroupVersion.String(),
		}

		manager := server.GetManagerInstance(c, scheme)
		clusters := manager.GetManagedSveltosClusters()
		_, ok := clusters[*cluster]
		Expect(ok).To(BeTrue())

		// Delete SveltosCluster
		Expect(c.Delete(context.TODO(), sveltosCluster)).To(Succeed())

		_, err = reconciler.Reconcile(context.TODO(), ctrl.Request{
			NamespacedName: sveltosClusterName,
		})
		Expect(err).ToNot(HaveOccurred())

		clusters = manager.GetManagedSveltosClusters()
		_, ok = clusters[*cluster]
		Expect(ok).To(BeFalse())

	})
})

func getSveltosClusterReconciler(c client.Client) *controller.SveltosClusterReconciler {
	return &controller.SveltosClusterReconciler{
		Client: c,
		Scheme: scheme,
	}
}
