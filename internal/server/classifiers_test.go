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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/utils/ptr"

	libsveltosv1beta1 "github.com/projectsveltos/libsveltos/api/v1beta1"
	"github.com/projectsveltos/ui-backend/internal/server"
)

const (
	testEnvLabelKey    = "env"
	testTeamLabelKey   = "team"
	testEnvValue       = "prod"
	testTeamValue      = "platform"
	testNamespace      = "default"
	testClusterName    = "workload"
	testMgmtName       = "mgmt"
	testClassifierName = "default-classifier"
	testConflictOwner  = "classifier other-classifier currently manages this"
	testNoMatchValue   = "no-such-value"
)

var _ = Describe("Classifier Data", func() {
	var classifierLabels []libsveltosv1beta1.ClassifierLabel

	BeforeEach(func() {
		classifierLabels = []libsveltosv1beta1.ClassifierLabel{
			{Key: testEnvLabelKey, Value: testEnvValue},
			{Key: testTeamLabelKey, Value: testTeamValue},
		}
	})

	It("matchingClustersFromClassifierReports joins label values and skips non-matching/phantom reports", func() {
		matchingReport := libsveltosv1beta1.ClassifierReport{
			Spec: libsveltosv1beta1.ClassifierReportSpec{
				ClusterNamespace: testNamespace,
				ClusterName:      testClusterName,
				ClusterType:      libsveltosv1beta1.ClusterTypeCapi,
				Match:            true,
			},
			Status: libsveltosv1beta1.ClassifierReportStatus{
				ManagedLabels: []string{testEnvLabelKey},
			},
		}

		conflictedReport := libsveltosv1beta1.ClassifierReport{
			Spec: libsveltosv1beta1.ClassifierReportSpec{
				ClusterNamespace: testMgmtName,
				ClusterName:      testMgmtName,
				ClusterType:      libsveltosv1beta1.ClusterTypeSveltos,
				Match:            true,
			},
			Status: libsveltosv1beta1.ClassifierReportStatus{
				ManagedLabels: []string{testTeamLabelKey},
				UnManagedLabels: []libsveltosv1beta1.UnManagedLabel{
					{Key: testEnvLabelKey, FailureMessage: ptr.To(testConflictOwner)},
				},
			},
		}

		// Cluster no longer a match: must not be reported as a matching cluster.
		nonMatchingReport := libsveltosv1beta1.ClassifierReport{
			Spec: libsveltosv1beta1.ClassifierReportSpec{
				ClusterNamespace: testNamespace,
				ClusterName:      "stale",
				ClusterType:      libsveltosv1beta1.ClusterTypeCapi,
				Match:            false,
			},
		}

		// Placeholder report with no cluster associated yet (observed live: phase
		// WaitingForDelivery, empty cluster fields, match true). Must be skipped.
		phantomReport := libsveltosv1beta1.ClassifierReport{
			Spec: libsveltosv1beta1.ClassifierReportSpec{
				Match: true,
			},
		}

		reports := []libsveltosv1beta1.ClassifierReport{
			matchingReport, conflictedReport, nonMatchingReport, phantomReport,
		}

		result := server.MatchingClustersFromClassifierReports(reports, classifierLabels)
		Expect(result).To(HaveLen(2))

		// Sorted by namespace then name: "default/workload" before "mgmt/mgmt".
		Expect(result[0].ClusterNamespace).To(Equal(testNamespace))
		Expect(result[0].ClusterName).To(Equal(testClusterName))
		Expect(result[0].ManagedLabels).To(ConsistOf(server.ClassifierLabelEntry{Key: testEnvLabelKey, Value: testEnvValue}))
		Expect(result[0].ConflictedLabels).To(BeEmpty())

		Expect(result[1].ClusterNamespace).To(Equal(testMgmtName))
		Expect(result[1].ClusterName).To(Equal(testMgmtName))
		Expect(result[1].ManagedLabels).To(ConsistOf(server.ClassifierLabelEntry{Key: testTeamLabelKey, Value: testTeamValue}))
		Expect(result[1].ConflictedLabels).To(ConsistOf(server.UnManagedLabelEntry{
			Key: testEnvLabelKey, FailureMessage: testConflictOwner,
		}))
	})

	It("matchingClustersFromManagementClusterClassifierReports skips reports with no cluster associated", func() {
		matchingReport := libsveltosv1beta1.ManagementClusterClassifierReport{
			Spec: libsveltosv1beta1.ManagementClusterClassifierReportSpec{
				ClusterNamespace: testNamespace,
				ClusterName:      testClusterName,
				ClusterType:      libsveltosv1beta1.ClusterTypeCapi,
			},
			Status: libsveltosv1beta1.ManagementClusterClassifierReportStatus{
				ManagedLabels: []string{testEnvLabelKey, testTeamLabelKey},
			},
		}

		phantomReport := libsveltosv1beta1.ManagementClusterClassifierReport{}

		reports := []libsveltosv1beta1.ManagementClusterClassifierReport{matchingReport, phantomReport}

		result := server.MatchingClustersFromManagementClusterClassifierReports(reports, classifierLabels)
		Expect(result).To(HaveLen(1))
		Expect(result[0].ClusterName).To(Equal(testClusterName))
		Expect(result[0].ManagedLabels).To(ConsistOf(
			server.ClassifierLabelEntry{Key: testEnvLabelKey, Value: testEnvValue},
			server.ClassifierLabelEntry{Key: testTeamLabelKey, Value: testTeamValue},
		))
	})

	It("classifierReportsMatchCluster requires Match to be true", func() {
		reports := []libsveltosv1beta1.ClassifierReport{
			{
				Spec: libsveltosv1beta1.ClassifierReportSpec{
					ClusterNamespace: testNamespace, ClusterName: testClusterName,
					ClusterType: libsveltosv1beta1.ClusterTypeCapi, Match: false,
				},
			},
		}
		filters := &server.ClassifierFilters{
			ClusterNamespace: testNamespace, ClusterName: testClusterName,
		}
		Expect(server.ClassifierReportsMatchCluster(reports, filters)).To(BeFalse())

		reports[0].Spec.Match = true
		Expect(server.ClassifierReportsMatchCluster(reports, filters)).To(BeTrue())

		otherFilters := &server.ClassifierFilters{
			ClusterNamespace: testNamespace, ClusterName: "other",
		}
		Expect(server.ClassifierReportsMatchCluster(reports, otherFilters)).To(BeFalse())
	})

	It("classifierReportsMatchCluster matches on a partially specified filter", func() {
		reports := []libsveltosv1beta1.ClassifierReport{
			{
				Spec: libsveltosv1beta1.ClassifierReportSpec{
					ClusterNamespace: testNamespace, ClusterName: testClusterName,
					ClusterType: libsveltosv1beta1.ClusterTypeCapi, Match: true,
				},
			},
		}

		// Only cluster_namespace set: must still match, not require cluster_name too.
		namespaceOnly := &server.ClassifierFilters{ClusterNamespace: testNamespace}
		Expect(server.ClassifierReportsMatchCluster(reports, namespaceOnly)).To(BeTrue())

		// Only cluster_name set.
		nameOnly := &server.ClassifierFilters{ClusterName: testClusterName}
		Expect(server.ClassifierReportsMatchCluster(reports, nameOnly)).To(BeTrue())

		// Substring match on namespace.
		substringNamespace := &server.ClassifierFilters{ClusterNamespace: "efau"}
		Expect(server.ClassifierReportsMatchCluster(reports, substringNamespace)).To(BeTrue())

		// A field that doesn't match still excludes the report even if others are unset.
		wrongName := &server.ClassifierFilters{ClusterName: testNoMatchValue}
		Expect(server.ClassifierReportsMatchCluster(reports, wrongName)).To(BeFalse())
	})

	It("classifierNameMatches applies a case-insensitive substring match on the classifier name", func() {
		Expect(server.ClassifierNameMatches(testClassifierName, &server.ClassifierFilters{})).To(BeTrue())
		Expect(
			server.ClassifierNameMatches(testClassifierName, &server.ClassifierFilters{Name: "DEFAULT-CLASSIF"}),
		).To(BeTrue())
		Expect(
			server.ClassifierNameMatches(testClassifierName, &server.ClassifierFilters{Name: testNoMatchValue}),
		).To(BeFalse())
	})

	It("managementClusterClassifierReportsMatchCluster ignores reports with no cluster associated", func() {
		reports := []libsveltosv1beta1.ManagementClusterClassifierReport{
			{},
		}
		filters := &server.ClassifierFilters{
			ClusterNamespace: testNamespace, ClusterName: testClusterName,
		}
		Expect(server.ManagementClusterClassifierReportsMatchCluster(reports, filters)).To(BeFalse())

		reports[0].Spec = libsveltosv1beta1.ManagementClusterClassifierReportSpec{
			ClusterNamespace: testNamespace, ClusterName: testClusterName, ClusterType: libsveltosv1beta1.ClusterTypeCapi,
		}
		Expect(server.ManagementClusterClassifierReportsMatchCluster(reports, filters)).To(BeTrue())

		// Only cluster_namespace set: must still match.
		namespaceOnly := &server.ClassifierFilters{ClusterNamespace: testNamespace}
		Expect(
			server.ManagementClusterClassifierReportsMatchCluster(reports, namespaceOnly),
		).To(BeTrue())
	})

	It("countMatchingClassifierReports and countMatchingManagementClusterClassifierReports exclude non-matches and phantoms", func() {
		classifierReports := []libsveltosv1beta1.ClassifierReport{
			{Spec: libsveltosv1beta1.ClassifierReportSpec{ClusterName: "a", Match: true}},
			{Spec: libsveltosv1beta1.ClassifierReportSpec{ClusterName: "b", Match: false}},
			{Spec: libsveltosv1beta1.ClassifierReportSpec{Match: true}}, // phantom: empty ClusterName
		}
		Expect(server.CountMatchingClassifierReports(classifierReports)).To(Equal(1))

		mccReports := []libsveltosv1beta1.ManagementClusterClassifierReport{
			{Spec: libsveltosv1beta1.ManagementClusterClassifierReportSpec{ClusterName: "a"}},
			{}, // phantom: empty ClusterName
		}
		Expect(server.CountMatchingManagementClusterClassifierReports(mccReports)).To(Equal(1))
	})
})
