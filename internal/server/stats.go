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
	"fmt"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	eventv1beta1 "github.com/projectsveltos/event-manager/api/v1beta1"
	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
)

// Stats holds counts of Sveltos resources accessible to the requesting user.
type Stats struct {
	CAPIClusters                 int `json:"capiClusters"`
	NotReadyCAPIClusters         int `json:"notReadyCAPIClusters"`
	SveltosClusters              int `json:"sveltosClusters"`
	NotReadySveltosClusters      int `json:"notReadySveltosClusters"`
	PullModeClusters             int `json:"pullModeClusters"`
	ClusterProfiles              int `json:"clusterProfiles"`
	Profiles                     int `json:"profiles"`
	ClusterSummaries             int `json:"clusterSummaries"`
	EventTriggers                int `json:"eventTriggers"`
	Classifiers                  int `json:"classifiers"`
	ManagementClusterClassifiers int `json:"managementClusterClassifiers"`
}

type clusterCounts struct {
	capiTotal       int
	capiNotReady    int
	sveltosTotal    int
	sveltosNotReady int
	pullMode        int
}

func (m *instance) getSveltosStats(ctx context.Context, user string) (Stats, error) {
	cc, err := m.countClusters(ctx, user)
	if err != nil {
		return Stats{}, err
	}

	clusterProfiles, profiles, err := m.countClusterProfilesAndProfiles(ctx, user)
	if err != nil {
		return Stats{}, err
	}

	clusterSummaries, err := m.countClusterSummaries(ctx, user)
	if err != nil {
		return Stats{}, err
	}

	eventTriggers, err := m.countEventTriggers(ctx, user)
	if err != nil {
		return Stats{}, err
	}

	classifiers, err := m.countClassifiers(ctx, user)
	if err != nil {
		return Stats{}, err
	}

	managementClusterClassifiers, err := m.countManagementClusterClassifiers(ctx, user)
	if err != nil {
		return Stats{}, err
	}

	return Stats{
		CAPIClusters:                 cc.capiTotal,
		NotReadyCAPIClusters:         cc.capiNotReady,
		SveltosClusters:              cc.sveltosTotal,
		NotReadySveltosClusters:      cc.sveltosNotReady,
		PullModeClusters:             cc.pullMode,
		ClusterProfiles:              clusterProfiles,
		Profiles:                     profiles,
		ClusterSummaries:             clusterSummaries,
		EventTriggers:                eventTriggers,
		Classifiers:                  classifiers,
		ManagementClusterClassifiers: managementClusterClassifiers,
	}, nil
}

func (m *instance) countClusters(ctx context.Context, user string) (clusterCounts, error) {
	canListSveltos, err := m.canListSveltosClusters(user)
	if err != nil {
		return clusterCounts{}, err
	}
	canListCAPI, err := m.canListCAPIClusters(user)
	if err != nil {
		return clusterCounts{}, err
	}

	sveltos, err := m.GetManagedSveltosClusters(ctx, canListSveltos, user)
	if err != nil {
		return clusterCounts{}, err
	}
	capi, err := m.GetManagedCAPIClusters(ctx, canListCAPI, user)
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

func (m *instance) countClusterProfilesAndProfiles(ctx context.Context, user string) (clusterProfiles, profiles int, err error) {
	canListCP, err := m.canListClusterProfiles(user)
	if err != nil {
		return 0, 0, err
	}
	canListP, err := m.canListProfiles(user)
	if err != nil {
		return 0, 0, err
	}

	accessible, err := m.GetProfiles(ctx, canListCP, canListP, user)
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

func (m *instance) countClusterSummaries(ctx context.Context, user string) (int, error) {
	canList, err := m.canListClusterSummaries(user)
	if err != nil {
		return 0, err
	}

	if canList {
		m.clusterStatusesMux.RLock()
		defer m.clusterStatusesMux.RUnlock()
		return len(m.clusterSummaryReport), nil
	}

	summaries := &configv1beta1.ClusterSummaryList{}
	if err := m.client.List(ctx, summaries); err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to list ClusterSummaries: %v", err))
		return 0, err
	}

	count := 0
	for i := range summaries.Items {
		s := &summaries.Items[i]
		ok, err := m.canGetClusterSummary(s.Namespace, s.Name, user)
		if err != nil {
			continue
		}
		if ok {
			count++
		}
	}
	return count, nil
}

func (m *instance) countEventTriggers(ctx context.Context, user string) (int, error) {
	canList, err := m.canListEventTriggers(user)
	if err != nil {
		return 0, err
	}

	eventTriggers := &eventv1beta1.EventTriggerList{}
	if err := m.client.List(ctx, eventTriggers); err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to list EventTriggers: %v", err))
		return 0, err
	}

	count := 0
	for i := range eventTriggers.Items {
		et := &eventTriggers.Items[i]
		if !et.GetDeletionTimestamp().IsZero() {
			continue
		}
		if canList {
			count++
			continue
		}
		ok, err := m.canGetEventTrigger(et.Name, user)
		if err != nil {
			continue
		}
		if ok {
			count++
		}
	}
	return count, nil
}

func (m *instance) countClassifiers(ctx context.Context, user string) (int, error) {
	canList, err := m.canListClassifiers(user)
	if err != nil {
		return 0, err
	}

	classifiers := &libsveltosv1beta1.ClassifierList{}
	if err := m.client.List(ctx, classifiers); err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to list Classifiers: %v", err))
		return 0, err
	}

	count := 0
	for i := range classifiers.Items {
		classifier := &classifiers.Items[i]
		if !classifier.GetDeletionTimestamp().IsZero() {
			continue
		}
		if canList {
			count++
			continue
		}
		ok, err := m.canGetClassifier(classifier.Name, user)
		if err != nil {
			continue
		}
		if ok {
			count++
		}
	}
	return count, nil
}

func (m *instance) countManagementClusterClassifiers(ctx context.Context, user string) (int, error) {
	canList, err := m.canListManagementClusterClassifiers(user)
	if err != nil {
		return 0, err
	}

	mccs := &libsveltosv1beta1.ManagementClusterClassifierList{}
	if err := m.client.List(ctx, mccs); err != nil {
		m.logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to list ManagementClusterClassifiers: %v", err))
		return 0, err
	}

	count := 0
	for i := range mccs.Items {
		mcc := &mccs.Items[i]
		if !mcc.GetDeletionTimestamp().IsZero() {
			continue
		}
		if canList {
			count++
			continue
		}
		ok, err := m.canGetManagementClusterClassifier(mcc.Name, user)
		if err != nil {
			continue
		}
		if ok {
			count++
		}
	}
	return count, nil
}
