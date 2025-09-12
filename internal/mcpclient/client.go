/*
Copyright 2025. projectsveltos.io. All rights reserved.

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

package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"

	configv1beta1 "github.com/projectsveltos/addon-controller/api/v1beta1"
	logs "github.com/projectsveltos/libsveltos/lib/logsettings"
)

const (
	noStructureContentError = "tool returned no structured content"
)

// connect to Sveltos mcp server
func connect(ctx context.Context, url string, logger logr.Logger) (*mcp.ClientSession, error) {
	// Create the URL for the server.
	logger.V(logs.LogInfo).Info(fmt.Sprintf("Connecting to MCP server at %s", url))

	// Create an MCP client.
	client := mcp.NewClient(&mcp.Implementation{
		Name:    "sveltos-client",
		Version: "1.0.0",
	}, nil)

	// Connect to the server.
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	if err != nil {
		logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to connect: %v", err))
		return nil, err
	}

	logger.V(logs.LogDebug).Info(fmt.Sprintf("Connected to server (session ID: %s)", session.ID()))

	// First, list available tools.
	logger.V(logs.LogDebug).Info("Listing available tools...")
	toolsResult, err := session.ListTools(ctx, nil)
	if err != nil {
		logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to list tool: %v", err))
		return nil, err
	}

	for _, tool := range toolsResult.Tools {
		logger.V(logs.LogInfo).Info(fmt.Sprintf("  - %s: %s\n", tool.Name, tool.Description))
	}

	return session, nil
}

// SveltosInstallationResult reports the outcome of the Sveltos installation verification.
type SveltosInstallationResult struct {
	IsCorrectlyInstalled bool   `json:"is_correctly_installed"`
	Details              string `json:"details,omitempty"`
}

func CheckInstallation(ctx context.Context, url string, logger logr.Logger) (string, error) {
	session, err := connect(ctx, url, logger)
	if err != nil {
		logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to connect: %v", err))
		return "", err
	}
	defer session.Close()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "installation_status",
		Arguments: nil,
	})

	if err != nil {
		logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to invoked installation_status tool: %v", err))
		return "", err
	}

	if result.IsError {
		errorMsg := fmt.Sprintf("MCP installation_status returned error: %v", result.Content)
		logger.V(logs.LogInfo).Info(errorMsg)
		return "", errors.New(errorMsg)
	}

	// Check for structured content. It should contain our result.
	if result.StructuredContent == nil {
		errorMsg := noStructureContentError
		logger.V(logs.LogInfo).Info(errorMsg)
		return "", errors.New(errorMsg)
	}

	// Marshal the StructuredContent to JSON and then unmarshal it into our struct.
	// This is a common pattern for converting `any` to a specific type.
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		errorMsg := fmt.Sprintf("failed to marshal structured content: %v", err)
		logger.V(logs.LogInfo).Info(errorMsg)
		return "", errors.New(errorMsg)
	}

	var installationResult SveltosInstallationResult
	err = json.Unmarshal(data, &installationResult)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal structured content: %w", err)
	}

	logger.V(logs.LogInfo).Info(fmt.Sprintf("installation_status result: %v", installationResult))

	// Check the boolean field from the unmarshaled struct to get the result.
	if installationResult.IsCorrectlyInstalled {
		return "Sveltos installation is correct.", nil
	}

	return fmt.Sprintf("Sveltos installation is NOT correct. Details: %s", installationResult.Details), nil
}

// DeploymentError represents a single deployment failure for a Sveltos profile.
type DeploymentError struct {
	ProfileName string   `json:"profileName" jsonschema:"The name of the Sveltos profile that is failing"`
	ProfileKind string   `json:"profileKind" jsonschema:"The profile kind (ClusterProfile vs Profile)"`
	Causes      []string `json:"causes" jsonschema:"The reason for the deployment failure"`
}

func CheckProfileDeploymentOnCluster(ctx context.Context, url string, clusterRef,
	profileRef *corev1.ObjectReference, logger logr.Logger) (string, error) {

	session, err := connect(ctx, url, logger)
	if err != nil {
		logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to connect: %v", err))
		return "", err
	}
	defer session.Close()

	input := map[string]any{
		"clusterRef": map[string]any{
			"namespace":  clusterRef.Namespace,
			"name":       clusterRef.Name,
			"kind":       clusterRef.Kind,
			"apiVersion": clusterRef.APIVersion,
		},
		"profileRef": map[string]any{
			"namespace":  profileRef.Namespace,
			"name":       profileRef.Name,
			"kind":       profileRef.Kind,
			"apiVersion": profileRef.APIVersion,
		},
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "analyze_profile_deployment",
		Arguments: input,
	})

	if err != nil {
		logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to invoked analyze_profile_deployment tool: %v", err))
		return "", err
	}

	if result.IsError {
		errorMsg := fmt.Sprintf("MCP analyze_profile_deployment returned error: %v", result.Content)
		logger.V(logs.LogInfo).Info(errorMsg)
		return "", errors.New(errorMsg)
	}

	// Check for structured content. It should contain our result.
	if result.StructuredContent == nil {
		errorMsg := noStructureContentError
		logger.V(logs.LogInfo).Info(errorMsg)
		return "", errors.New(errorMsg)
	}

	// Marshal the StructuredContent to JSON and then unmarshal it into our struct.
	// This is a common pattern for converting `any` to a specific type.
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		errorMsg := fmt.Sprintf("failed to marshal structured content: %v", err)
		logger.V(logs.LogInfo).Info(errorMsg)
		return "", errors.New(errorMsg)
	}

	var deploymentResult DeploymentError
	err = json.Unmarshal(data, &deploymentResult)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal structured content: %w", err)
	}

	logger.V(logs.LogInfo).Info(fmt.Sprintf("analyze_profile_deployment result: %v", deploymentResult))

	profileName := profileRef.Name
	if profileRef.Kind == configv1beta1.ProfileKind {
		profileName = fmt.Sprintf("%s/%s", profileRef.Namespace, profileRef.Name)
	}

	// Check the boolean field from the unmarshaled struct to get the result.
	if len(deploymentResult.Causes) == 0 {
		return fmt.Sprintf("%s %s is properly deployed on Cluster %s %s/%s",
			profileRef.Kind, profileName,
			clusterRef.Kind, clusterRef.Namespace, clusterRef.Namespace), nil
	}

	return fmt.Sprintf("%s %s is not properly deployed on Cluster %s %s/%s. Following errors detected: %v",
		profileRef.Kind, profileName,
		clusterRef.Kind, clusterRef.Namespace, clusterRef.Namespace,
		deploymentResult.Causes), nil
}

type DeploymentErrors struct {
	Errors []DeploymentError `json:"deploymentErrors" jsonschema:"List of all resources that are being deployed, failing to deploy or whose deployment has failed"`
}

func CheckClusterDeploymentStatuses(ctx context.Context, url string, clusterRef *corev1.ObjectReference,
	logger logr.Logger) (string, error) {

	session, err := connect(ctx, url, logger)
	if err != nil {
		logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to connect: %v", err))
		return "", err
	}
	defer session.Close()

	input := map[string]any{
		"namespace":  clusterRef.Namespace,
		"name":       clusterRef.Name,
		"kind":       clusterRef.Kind,
		"apiVersion": clusterRef.APIVersion,
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_deployement_errors",
		Arguments: input,
	})

	if err != nil {
		logger.V(logs.LogInfo).Info(fmt.Sprintf("failed to invoked list_deployement_errors tool: %v", err))
		return "", err
	}

	if result.IsError {
		errorMsg := fmt.Sprintf("MCP list_deployement_errors returned error: %v", result.Content)
		logger.V(logs.LogInfo).Info(errorMsg)
		return "", errors.New(errorMsg)
	}

	// Check for structured content. It should contain our result.
	if result.StructuredContent == nil {
		errorMsg := noStructureContentError
		logger.V(logs.LogInfo).Info(errorMsg)
		return "", errors.New(errorMsg)
	}

	// Marshal the StructuredContent to JSON and then unmarshal it into our struct.
	// This is a common pattern for converting `any` to a specific type.
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		errorMsg := fmt.Sprintf("failed to marshal structured content: %v", err)
		logger.V(logs.LogInfo).Info(errorMsg)
		return "", errors.New(errorMsg)
	}

	var deploymentResult DeploymentErrors
	err = json.Unmarshal(data, &deploymentResult)
	if err != nil {
		return "", fmt.Errorf("failed to unmarshal structured content: %w", err)
	}

	logger.V(logs.LogInfo).Info(fmt.Sprintf("analyze_profile_deployment result: %v", deploymentResult))

	if len(deploymentResult.Errors) == 0 {
		return fmt.Sprintf("all matching profiles are successfully deployed on cluster %s %s/%s",
			clusterRef.Kind, clusterRef.Namespace, clusterRef.Namespace), nil
	}

	detectedErrors := ""

	for i := range deploymentResult.Errors {
		detectedErrors += fmt.Sprintf("%s %s is not properly deployed. Following errors detected: %v",
			deploymentResult.Errors[i].ProfileKind, deploymentResult.Errors[i].ProfileName,
			deploymentResult.Errors[i].Causes)
	}

	return detectedErrors, nil
}
