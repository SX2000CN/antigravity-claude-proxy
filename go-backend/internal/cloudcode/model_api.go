// Package cloudcode provides Cloud Code API client implementation.
// This file corresponds to src/cloudcode/model-api.js in the Node.js version.
package cloudcode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// Model validation cache
var modelCache = struct {
	sync.RWMutex
	validModels  map[string]bool
	lastFetched  time.Time
	fetchPromise chan struct{}
}{
	validModels: make(map[string]bool),
}

// ModelInfo represents model information from the API
type ModelInfo struct {
	DisplayName string     `json:"displayName,omitempty"`
	QuotaInfo   *QuotaInfo `json:"quotaInfo,omitempty"`
}

// QuotaInfo represents quota information for a model
type QuotaInfo struct {
	RemainingFraction *float64 `json:"remainingFraction,omitempty"`
	ResetTime         *string  `json:"resetTime,omitempty"`
}

// FetchModelsResponse represents the response from fetchAvailableModels API
type FetchModelsResponse struct {
	Models map[string]*ModelInfo `json:"models,omitempty"`
}

// ModelListResponse represents the response in Anthropic format
type ModelListResponse struct {
	Object string       `json:"object"`
	Data   []ModelEntry `json:"data"`
}

// ModelEntry represents a single model entry
type ModelEntry struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	Description string `json:"description"`
}

// ModelQuota represents quota info for a model
type ModelQuota struct {
	RemainingFraction *float64 `json:"remainingFraction,omitempty"`
	ResetTime         *string  `json:"resetTime,omitempty"`
}

// SubscriptionInfo represents subscription tier and project info
type SubscriptionInfo struct {
	Tier      string `json:"tier"`
	ProjectID string `json:"projectId,omitempty"`
}

// LoadCodeAssistRequest represents the request body for loadCodeAssist API
type LoadCodeAssistRequest struct {
	Metadata *LoadCodeAssistMetadata `json:"metadata,omitempty"`
}

// LoadCodeAssistMetadata represents metadata in the loadCodeAssist request
type LoadCodeAssistMetadata struct {
	IDEType     string `json:"ideType,omitempty"`
	Platform    string `json:"platform,omitempty"`
	PluginType  string `json:"pluginType,omitempty"`
	DuetProject string `json:"duetProject,omitempty"`
}

// LoadCodeAssistResponse represents the response from loadCodeAssist API
type LoadCodeAssistResponse struct {
	PaidTier                *TierInfo   `json:"paidTier,omitempty"`
	CurrentTier             *TierInfo   `json:"currentTier,omitempty"`
	AllowedTiers            []*TierInfo `json:"allowedTiers,omitempty"`
	CloudAICompanionProject interface{} `json:"cloudaicompanionProject,omitempty"`
}

// TierInfo represents tier information
type TierInfo struct {
	ID        string `json:"id,omitempty"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

// isSupportedModel checks if a model is supported (Claude or Gemini)
func isSupportedModel(modelID string) bool {
	family := config.GetModelFamily(modelID)
	return family == config.ModelFamilyClaude || family == config.ModelFamilyGemini
}

// ListModels lists available models in Anthropic API format
func ListModels(ctx context.Context, token string) (*ModelListResponse, error) {
	data, err := FetchAvailableModels(ctx, token, "")
	if err != nil {
		return nil, err
	}

	if data == nil || data.Models == nil {
		return &ModelListResponse{Object: "list", Data: []ModelEntry{}}, nil
	}

	now := time.Now().Unix()
	modelList := make([]ModelEntry, 0)

	for modelID, modelData := range data.Models {
		if !isSupportedModel(modelID) {
			continue
		}

		description := modelID
		if modelData != nil && modelData.DisplayName != "" {
			description = modelData.DisplayName
		}

		modelList = append(modelList, ModelEntry{
			ID:          modelID,
			Object:      "model",
			Created:     now,
			OwnedBy:     "anthropic",
			Description: description,
		})
	}

	// Warm the model validation cache
	modelCache.Lock()
	modelCache.validModels = make(map[string]bool)
	for _, m := range modelList {
		modelCache.validModels[m.ID] = true
	}
	modelCache.lastFetched = time.Now()
	modelCache.Unlock()

	return &ModelListResponse{
		Object: "list",
		Data:   modelList,
	}, nil
}

// FetchAvailableModels fetches available models with quota info from Cloud Code API
func FetchAvailableModels(ctx context.Context, token, projectID string) (*FetchModelsResponse, error) {
	headers := make(map[string]string)
	headers["Authorization"] = "Bearer " + token
	headers["Content-Type"] = "application/json"
	for k, v := range config.AntigravityHeaders() {
		headers[k] = v
	}

	// Include project ID in body for accurate quota info
	body := make(map[string]string)
	if projectID != "" {
		body["project"] = projectID
	}
	bodyBytes, _ := json.Marshal(body)

	for _, endpoint := range config.AntigravityEndpointFallbacks {
		url := endpoint + "/v1internal:fetchAvailableModels"

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
		if err != nil {
			continue
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			utils.Warn("[CloudCode] fetchAvailableModels failed at %s: %v", endpoint, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			utils.Warn("[CloudCode] fetchAvailableModels error at %s: %d", endpoint, resp.StatusCode)
			continue
		}

		var data FetchModelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			utils.Warn("[CloudCode] fetchAvailableModels decode error at %s: %v", endpoint, err)
			continue
		}

		return &data, nil
	}

	return nil, fmt.Errorf("Failed to fetch available models from all endpoints")
}

// GetModelQuotas gets model quotas for an account
func GetModelQuotas(ctx context.Context, token, projectID string) (map[string]*ModelQuota, error) {
	data, err := FetchAvailableModels(ctx, token, projectID)
	if err != nil {
		return nil, err
	}

	if data == nil || data.Models == nil {
		return make(map[string]*ModelQuota), nil
	}

	quotas := make(map[string]*ModelQuota)
	for modelID, modelData := range data.Models {
		// Only include Claude and Gemini models
		if !isSupportedModel(modelID) {
			continue
		}

		if modelData != nil && modelData.QuotaInfo != nil {
			quota := &ModelQuota{
				ResetTime: modelData.QuotaInfo.ResetTime,
			}

			// When remainingFraction is missing but resetTime is present, quota is exhausted (0%)
			if modelData.QuotaInfo.RemainingFraction != nil {
				quota.RemainingFraction = modelData.QuotaInfo.RemainingFraction
			} else if modelData.QuotaInfo.ResetTime != nil {
				zero := 0.0
				quota.RemainingFraction = &zero
			}

			quotas[modelID] = quota
		}
	}

	return quotas, nil
}

// ParseTierID parses tier ID string to determine subscription level
func ParseTierID(tierID string) string {
	if tierID == "" {
		return "unknown"
	}

	lower := strings.ToLower(tierID)

	if strings.Contains(lower, "ultra") {
		return "ultra"
	}
	if lower == "standard-tier" {
		// standard-tier = "Gemini Code Assist" (paid, project-based)
		return "pro"
	}
	if strings.Contains(lower, "pro") || strings.Contains(lower, "premium") {
		return "pro"
	}
	if lower == "free-tier" || strings.Contains(lower, "free") {
		return "free"
	}
	return "unknown"
}

// GetSubscriptionTier gets subscription tier for an account
func GetSubscriptionTier(ctx context.Context, token string) (*SubscriptionInfo, error) {
	headers := make(map[string]string)
	headers["Authorization"] = "Bearer " + token
	headers["Content-Type"] = "application/json"
	for k, v := range config.LoadCodeAssistHeaders() {
		headers[k] = v
	}

	reqBody := &LoadCodeAssistRequest{
		Metadata: &LoadCodeAssistMetadata{
			IDEType:     "IDE_UNSPECIFIED",
			Platform:    "PLATFORM_UNSPECIFIED",
			PluginType:  "GEMINI",
			DuetProject: config.DefaultProjectID,
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	for _, endpoint := range config.LoadCodeAssistEndpoints {
		url := endpoint + "/v1internal:loadCodeAssist"

		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
		if err != nil {
			continue
		}

		for k, v := range headers {
			req.Header.Set(k, v)
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			utils.Warn("[CloudCode] loadCodeAssist failed at %s: %v", endpoint, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			utils.Warn("[CloudCode] loadCodeAssist error at %s: %d", endpoint, resp.StatusCode)
			continue
		}

		var data LoadCodeAssistResponse
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			utils.Warn("[CloudCode] loadCodeAssist decode error at %s: %v", endpoint, err)
			continue
		}

		// Extract project ID
		var projectID string
		switch v := data.CloudAICompanionProject.(type) {
		case string:
			projectID = v
		case map[string]interface{}:
			if id, ok := v["id"].(string); ok {
				projectID = id
			}
		}

		// Extract subscription tier
		// Priority: paidTier > currentTier > allowedTiers
		tier := "unknown"
		var tierID, tierSource string

		// 1. Check paidTier first
		if data.PaidTier != nil && data.PaidTier.ID != "" {
			tierID = data.PaidTier.ID
			tier = ParseTierID(tierID)
			tierSource = "paidTier"
		}

		// 2. Fall back to currentTier
		if tier == "unknown" && data.CurrentTier != nil && data.CurrentTier.ID != "" {
			tierID = data.CurrentTier.ID
			tier = ParseTierID(tierID)
			tierSource = "currentTier"
		}

		// 3. Fall back to allowedTiers
		if tier == "unknown" && len(data.AllowedTiers) > 0 {
			var defaultTier *TierInfo
			for _, t := range data.AllowedTiers {
				if t != nil && t.IsDefault {
					defaultTier = t
					break
				}
			}
			if defaultTier == nil && data.AllowedTiers[0] != nil {
				defaultTier = data.AllowedTiers[0]
			}
			if defaultTier != nil && defaultTier.ID != "" {
				tierID = defaultTier.ID
				tier = ParseTierID(tierID)
				tierSource = "allowedTiers"
			}
		}

		utils.Debug("[CloudCode] Subscription detected: %s (tierId: %s, source: %s), Project: %s",
			tier, tierID, tierSource, projectID)

		return &SubscriptionInfo{
			Tier:      tier,
			ProjectID: projectID,
		}, nil
	}

	// Fallback: return default values if all endpoints fail
	utils.Warn("[CloudCode] Failed to detect subscription tier from all endpoints. Defaulting to free.")
	return &SubscriptionInfo{Tier: "free", ProjectID: ""}, nil
}

// PopulateModelCache populates the model validation cache
func PopulateModelCache(ctx context.Context, token, projectID string) error {
	now := time.Now()

	modelCache.RLock()
	cacheSize := len(modelCache.validModels)
	lastFetched := modelCache.lastFetched
	modelCache.RUnlock()

	// Check if cache is fresh
	if cacheSize > 0 && now.Sub(lastFetched) < time.Duration(config.ModelValidationCacheTTLMs)*time.Millisecond {
		return nil
	}

	// Fetch and populate
	data, err := FetchAvailableModels(ctx, token, projectID)
	if err != nil {
		utils.Warn("[CloudCode] Failed to populate model cache: %v", err)
		return err
	}

	if data != nil && data.Models != nil {
		modelCache.Lock()
		modelCache.validModels = make(map[string]bool)
		for modelID := range data.Models {
			if isSupportedModel(modelID) {
				modelCache.validModels[modelID] = true
			}
		}
		modelCache.lastFetched = time.Now()
		utils.Debug("[CloudCode] Model cache populated with %d models", len(modelCache.validModels))
		modelCache.Unlock()
	}

	return nil
}

// IsValidModel checks if a model ID is valid
func IsValidModel(ctx context.Context, modelID, token, projectID string) bool {
	// Try to populate cache if needed
	_ = PopulateModelCache(ctx, token, projectID)

	modelCache.RLock()
	defer modelCache.RUnlock()

	// If cache is populated, validate against it
	if len(modelCache.validModels) > 0 {
		return modelCache.validModels[modelID]
	}

	// Cache empty (fetch failed) - fail open, let API validate
	return true
}
