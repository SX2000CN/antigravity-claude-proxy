// Package auth provides user onboarding for Antigravity.
// This file corresponds to src/account-manager/onboarding.js in the Node.js version.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// OnboardUser provisions a managed project for an account
// tierId: Tier ID (raw API value, e.g., 'free-tier', 'standard-tier', 'g1-pro-tier')
// projectID: Optional GCP project ID (required for non-free tiers)
func OnboardUser(ctx context.Context, token, tierId, projectID string, maxAttempts int, delayMs int64) (string, error) {
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	if delayMs <= 0 {
		delayMs = 5000
	}

	metadata := map[string]string{
		"ideType":    "IDE_UNSPECIFIED",
		"platform":   "PLATFORM_UNSPECIFIED",
		"pluginType": "GEMINI",
	}

	if projectID != "" {
		metadata["duetProject"] = projectID
	}

	requestBody := map[string]interface{}{
		"tierId":   tierId,
		"metadata": metadata,
	}
	// Note: Do NOT add cloudaicompanionProject to requestBody
	// Reference implementation only sets metadata.duetProject, not the body field
	// Adding cloudaicompanionProject causes 400 errors for auto-provisioned tiers (g1-pro, g1-ultra)

	utils.Debug("[Onboarding] Starting onboard with tierId: %s, projectID: %s", tierId, projectID)

	for _, endpoint := range config.OnboardUserEndpoints {
		for attempt := 0; attempt < maxAttempts; attempt++ {
			result, err := tryOnboardUser(ctx, endpoint, token, requestBody)
			if err != nil {
				utils.Warn("[Onboarding] onboardUser failed at %s: %v", endpoint, err)
				break // Try next endpoint
			}

			utils.Debug("[Onboarding] onboardUser response (attempt %d): %v", attempt+1, result)

			// Check if onboarding is complete
			if done, ok := result["done"].(bool); ok && done {
				// Try to get managed project ID
				if response, ok := result["response"].(map[string]interface{}); ok {
					if proj, ok := response["cloudaicompanionProject"].(map[string]interface{}); ok {
						if id, ok := proj["id"].(string); ok && id != "" {
							return id, nil
						}
					}
				}
				// If no managed project but we have a provided projectID
				if projectID != "" {
					return projectID, nil
				}
			}

			// Not done yet, wait and retry
			if attempt < maxAttempts-1 {
				utils.Debug("[Onboarding] onboardUser not complete, waiting %dms...", delayMs)
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(time.Duration(delayMs) * time.Millisecond):
				}
			}
		}
	}

	utils.Warn("[Onboarding] All onboarding attempts failed for tierId: %s", tierId)
	return "", fmt.Errorf("all onboarding attempts failed")
}

// tryOnboardUser attempts to onboard at a single endpoint
func tryOnboardUser(ctx context.Context, endpoint, token string, requestBody map[string]interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/v1internal:onboardUser", strings.NewReader(string(jsonBody)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range config.AntigravityHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result, nil
}
