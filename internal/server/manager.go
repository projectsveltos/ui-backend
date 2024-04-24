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
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"

	libsveltosv1alpha1 "github.com/projectsveltos/libsveltos/api/v1alpha1"
)

type ClusterInfo struct {
	Labels         map[string]string `json:"labels"`
	Version        string            `json:"version"`
	Ready          bool              `json:"ready"`
	FailureMessage *string           `json:"failureMessage"`
}

type instance struct {
	client     client.Client
	scheme     *runtime.Scheme
	clusterMux sync.Mutex // use a Mutex to update managed Clusters

	sveltosClusters map[corev1.ObjectReference]ClusterInfo
	capiClusters    map[corev1.ObjectReference]ClusterInfo
}

var (
	managerInstance *instance
	lock            = &sync.Mutex{}
)

// InitializeManagerInstance initializes manager instance
func InitializeManagerInstance(c client.Client, scheme *runtime.Scheme, port string, logger logr.Logger) {
	if managerInstance == nil {
		lock.Lock()
		defer lock.Unlock()
		if managerInstance == nil {
			managerInstance = &instance{
				client:          c,
				sveltosClusters: make(map[corev1.ObjectReference]ClusterInfo),
				capiClusters:    make(map[corev1.ObjectReference]ClusterInfo),
				clusterMux:      sync.Mutex{},
				scheme:          scheme,
			}

			go func() {
				managerInstance.start(port, logger)
			}()
		}
	}
}

func GetManagerInstance() *instance {
	return managerInstance
}

func (m *instance) GetManagedSveltosClusters() map[corev1.ObjectReference]ClusterInfo {
	lock.Lock()
	defer lock.Unlock()
	return m.sveltosClusters
}

func (m *instance) GetManagedCAPIClusters() map[corev1.ObjectReference]ClusterInfo {
	lock.Lock()
	defer lock.Unlock()
	return m.capiClusters
}

func (m *instance) AddSveltosCluster(sveltosCluster *libsveltosv1alpha1.SveltosCluster) {
	info := ClusterInfo{
		Labels:         sveltosCluster.Labels,
		Version:        sveltosCluster.Status.Version,
		Ready:          sveltosCluster.Status.Ready,
		FailureMessage: sveltosCluster.Status.FailureMessage,
	}

	sveltosClusterInfo := getKeyFromObject(m.scheme, sveltosCluster)

	m.clusterMux.Lock()
	defer m.clusterMux.Unlock()

	delete(m.sveltosClusters, *sveltosClusterInfo)
	m.sveltosClusters[*sveltosClusterInfo] = info
}

func (m *instance) RemoveSveltosCluster(sveltosClusterNamespace, sveltosClusterName string) {
	sveltosClusterInfo := &corev1.ObjectReference{
		Namespace:  sveltosClusterNamespace,
		Name:       sveltosClusterName,
		Kind:       libsveltosv1alpha1.SveltosClusterKind,
		APIVersion: libsveltosv1alpha1.GroupVersion.String(),
	}
	m.clusterMux.Lock()
	defer m.clusterMux.Unlock()

	delete(m.sveltosClusters, *sveltosClusterInfo)
}

func (m *instance) AddCAPICluster(cluster *clusterv1.Cluster) {
	info := ClusterInfo{
		Labels:         cluster.Labels,
		Ready:          cluster.Status.ControlPlaneReady,
		FailureMessage: cluster.Status.FailureMessage,
	}
	if cluster.Spec.Topology != nil {
		info.Version = cluster.Spec.Topology.Version
	}

	clusterInfo := getKeyFromObject(m.scheme, cluster)

	m.clusterMux.Lock()
	defer m.clusterMux.Unlock()

	delete(m.capiClusters, *clusterInfo)
	m.capiClusters[*clusterInfo] = info
}

func (m *instance) RemoveCAPICluster(clusterNamespace, clusterName string) {
	clusterInfo := &corev1.ObjectReference{
		Namespace:  clusterNamespace,
		Name:       clusterName,
		Kind:       "Cluster",
		APIVersion: clusterv1.GroupVersion.String(),
	}
	m.clusterMux.Lock()
	defer m.clusterMux.Unlock()

	delete(m.capiClusters, *clusterInfo)
}

// getKeyFromObject returns the Key that can be used in the internal reconciler maps.
func getKeyFromObject(scheme *runtime.Scheme, obj client.Object) *corev1.ObjectReference {
	addTypeInformationToObject(scheme, obj)

	apiVersion, kind := obj.GetObjectKind().GroupVersionKind().ToAPIVersionAndKind()

	return &corev1.ObjectReference{
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
		Kind:       kind,
		APIVersion: apiVersion,
	}
}

func addTypeInformationToObject(scheme *runtime.Scheme, obj client.Object) {
	gvks, _, err := scheme.ObjectKinds(obj)
	if err != nil {
		panic(1)
	}

	for _, gvk := range gvks {
		if gvk.Kind == "" {
			continue
		}
		if gvk.Version == "" || gvk.Version == runtime.APIVersionInternal {
			continue
		}
		obj.GetObjectKind().SetGroupVersionKind(gvk)
		break
	}
}
