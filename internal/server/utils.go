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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1" //nolint:staticcheck // SA1019: We are unable to update the dependency at this time.
)

func examineClusterConditions(cluster *clusterv1.Cluster) *string {
	if cluster == nil {
		return nil
	}

	message := ""

	for i := range cluster.Status.Conditions {
		c := cluster.Status.Conditions[i]
		if c.Status == corev1.ConditionFalse && c.Message != "" {
			message = fmt.Sprintf("%s\n%s", message, c.Message)
		}
	}

	if message != "" {
		return &message
	}

	return nil
}
