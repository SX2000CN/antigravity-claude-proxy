// Package auth provides Google OAuth authentication with PKCE for Antigravity.
// This file corresponds to src/auth/oauth.js in the Node.js version.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/poemonsense/antigravity-proxy-go/internal/config"
	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// RefreshParts represents the components of a composite refresh token
// Format: refreshToken|projectId|managedProjectId
type RefreshParts struct {
	RefreshToken     string
	ProjectID        string
	ManagedProjectID string
}

// ParseRefreshParts parses a composite refresh token string
func ParseRefreshParts(refresh string) RefreshParts {
	parts := strings.Split(refresh, "|")
	result := RefreshParts{}

	if len(parts) > 0 {
		result.RefreshToken = parts[0]
	}
	if len(parts) > 1 && parts[1] != "" {
		result.ProjectID = parts[1]
	}
	if len(parts) > 2 && parts[2] != "" {
		result.ManagedProjectID = parts[2]
	}

	return result
}

// FormatRefreshParts formats refresh token parts back into composite string
func FormatRefreshParts(parts RefreshParts) string {
	projectSegment := parts.ProjectID
	base := fmt.Sprintf("%s|%s", parts.RefreshToken, projectSegment)
	if parts.ManagedProjectID != "" {
		return fmt.Sprintf("%s|%s", base, parts.ManagedProjectID)
	}
	return base
}

// PKCE holds the PKCE code verifier and challenge
type PKCE struct {
	Verifier  string
	Challenge string
}

// GeneratePKCE generates a PKCE code verifier and challenge
func GeneratePKCE() (*PKCE, error) {
	// Generate 32 random bytes
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// URL-safe base64 encoding without padding
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	// SHA256 hash of verifier, then base64url encode
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	return &PKCE{
		Verifier:  verifier,
		Challenge: challenge,
	}, nil
}

// GenerateState generates a random state parameter for CSRF protection
func GenerateState() (string, error) {
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return hex.EncodeToString(stateBytes), nil
}

// AuthorizationURLResult contains the authorization URL and PKCE data
type AuthorizationURLResult struct {
	URL      string
	Verifier string
	State    string
}

// GetAuthorizationURL generates the authorization URL for Google OAuth
func GetAuthorizationURL(customRedirectURI string) (*AuthorizationURLResult, error) {
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}

	state, err := GenerateState()
	if err != nil {
		return nil, err
	}

	redirectURI := customRedirectURI
	if redirectURI == "" {
		redirectURI = fmt.Sprintf("http://localhost:%d/oauth-callback", config.OAuthCallbackPort)
	}

	params := url.Values{
		"client_id":             {config.OAuthClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {strings.Join(config.OAuthScopes, " ")},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"code_challenge":        {pkce.Challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}

	return &AuthorizationURLResult{
		URL:      fmt.Sprintf("%s?%s", config.OAuthAuthURL, params.Encode()),
		Verifier: pkce.Verifier,
		State:    state,
	}, nil
}

// CodeExtractResult contains the extracted authorization code and optional state
type CodeExtractResult struct {
	Code  string
	State string
}

// ExtractCodeFromInput extracts authorization code and state from user input
// User can paste either:
// - Full callback URL: http://localhost:51121/oauth-callback?code=xxx&state=xxx
// - Just the code parameter: 4/0xxx...
func ExtractCodeFromInput(input string) (*CodeExtractResult, error) {
	if input == "" {
		return nil, fmt.Errorf("no input provided")
	}

	trimmed := strings.TrimSpace(input)

	// Check if it looks like a URL
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid URL format")
		}

		errorParam := parsed.Query().Get("error")
		if errorParam != "" {
			return nil, fmt.Errorf("OAuth error: %s", errorParam)
		}

		code := parsed.Query().Get("code")
		if code == "" {
			return nil, fmt.Errorf("no authorization code found in URL")
		}

		return &CodeExtractResult{
			Code:  code,
			State: parsed.Query().Get("state"),
		}, nil
	}

	// Assume it's a raw code
	// Google auth codes typically start with "4/" and are long
	if len(trimmed) < 10 {
		return nil, fmt.Errorf("input is too short to be a valid authorization code")
	}

	return &CodeExtractResult{
		Code:  trimmed,
		State: "",
	}, nil
}

// CallbackServer represents the OAuth callback server
type CallbackServer struct {
	server     *http.Server
	mu         sync.Mutex
	actualPort int
	isAborted  bool
	codeChan   chan string
	errChan    chan error
}

// NewCallbackServer creates a new callback server
func NewCallbackServer(expectedState string, timeoutMs int) *CallbackServer {
	if timeoutMs <= 0 {
		timeoutMs = 120000
	}

	cs := &CallbackServer{
		actualPort: config.OAuthCallbackPort,
		codeChan:   make(chan string, 1),
		errChan:    make(chan error, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth-callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		errorParam := query.Get("error")
		if errorParam != "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `<html>
				<head><meta charset="UTF-8"><title>Authentication Failed</title></head>
				<body style="font-family: system-ui; padding: 40px; text-align: center;">
					<h1 style="color: #dc3545;">❌ Authentication Failed</h1>
					<p>Error: %s</p>
					<p>You can close this window.</p>
				</body>
			</html>`, errorParam)
			cs.errChan <- fmt.Errorf("OAuth error: %s", errorParam)
			return
		}

		state := query.Get("state")
		if state != expectedState {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<html>
				<head><meta charset="UTF-8"><title>Authentication Failed</title></head>
				<body style="font-family: system-ui; padding: 40px; text-align: center;">
					<h1 style="color: #dc3545;">❌ Authentication Failed</h1>
					<p>State mismatch - possible CSRF attack.</p>
					<p>You can close this window.</p>
				</body>
			</html>`)
			cs.errChan <- fmt.Errorf("state mismatch")
			return
		}

		code := query.Get("code")
		if code == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `<html>
				<head><meta charset="UTF-8"><title>Authentication Failed</title></head>
				<body style="font-family: system-ui; padding: 40px; text-align: center;">
					<h1 style="color: #dc3545;">❌ Authentication Failed</h1>
					<p>No authorization code received.</p>
					<p>You can close this window.</p>
				</body>
			</html>`)
			cs.errChan <- fmt.Errorf("no authorization code")
			return
		}

		// Success!
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `<html>
			<head><meta charset="UTF-8"><title>Authentication Successful</title></head>
			<body style="font-family: system-ui; padding: 40px; text-align: center;">
				<h1 style="color: #28a745;">✅ Authentication Successful!</h1>
				<p>You can close this window and return to the terminal.</p>
				<script>setTimeout(() => window.close(), 2000);</script>
			</body>
		</html>`)
		cs.codeChan <- code
	})

	cs.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	return cs
}

// Start starts the callback server and waits for the OAuth callback
func (cs *CallbackServer) Start(ctx context.Context) (string, error) {
	// Try ports with fallback logic
	portsToTry := append([]int{config.OAuthCallbackPort}, config.OAuthCallbackFallbackPorts...)

	var lastErr error
	for _, port := range portsToTry {
		cs.server.Addr = fmt.Sprintf(":%d", port)
		listener, err := cs.tryBind(port)
		if err != nil {
			lastErr = err
			utils.Warn("[OAuth] Failed to bind port %d: %v", port, err)
			continue
		}

		cs.actualPort = port
		if port != config.OAuthCallbackPort {
			utils.Warn("[OAuth] Primary port %d unavailable, using fallback port %d", config.OAuthCallbackPort, port)
		} else {
			utils.Info("[OAuth] Callback server listening on port %d", port)
		}

		// Start serving in background
		go func() {
			if err := cs.server.Serve(listener); err != nil && err != http.ErrServerClosed {
				cs.errChan <- err
			}
		}()

		// Wait for code, error, or context cancellation
		select {
		case code := <-cs.codeChan:
			cs.server.Shutdown(context.Background())
			return code, nil
		case err := <-cs.errChan:
			cs.server.Shutdown(context.Background())
			return "", err
		case <-ctx.Done():
			cs.server.Shutdown(context.Background())
			return "", ctx.Err()
		}
	}

	return "", fmt.Errorf("failed to start OAuth callback server: %v", lastErr)
}

// tryBind attempts to bind to a port
func (cs *CallbackServer) tryBind(port int) (net.Listener, error) {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	return listener, nil
}

// GetPort returns the actual port the server is listening on
func (cs *CallbackServer) GetPort() int {
	return cs.actualPort
}

// Abort aborts the callback server
func (cs *CallbackServer) Abort() {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.isAborted {
		return
	}
	cs.isAborted = true

	if cs.server != nil {
		cs.server.Shutdown(context.Background())
		utils.Info("[OAuth] Callback server aborted (manual completion)")
	}
}

// OAuthTokens represents the tokens returned from OAuth token exchange
type OAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// ExchangeCode exchanges an authorization code for tokens
func ExchangeCode(ctx context.Context, code, verifier string) (*OAuthTokens, error) {
	data := url.Values{
		"client_id":     {config.OAuthClientID},
		"client_secret": {config.OAuthClientSecret},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {fmt.Sprintf("http://localhost:%d/oauth-callback", config.OAuthCallbackPort)},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", config.OAuthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		utils.Error("[OAuth] Token exchange failed: %d %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	var tokens OAuthTokens
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokens.AccessToken == "" {
		utils.Error("[OAuth] No access token in response: %s", string(body))
		return nil, fmt.Errorf("no access token received")
	}

	utils.Info("[OAuth] Token exchange successful, access_token length: %d", len(tokens.AccessToken))

	return &tokens, nil
}

// RefreshResult represents the result of refreshing an access token
type RefreshResult struct {
	AccessToken string
	ExpiresIn   int
}

// RefreshAccessToken refreshes an access token using a refresh token
// Handles composite refresh tokens (refreshToken|projectId|managedProjectId)
func RefreshAccessToken(ctx context.Context, compositeRefresh string) (*RefreshResult, error) {
	parts := ParseRefreshParts(compositeRefresh)

	data := url.Values{
		"client_id":     {config.OAuthClientID},
		"client_secret": {config.OAuthClientSecret},
		"refresh_token": {parts.RefreshToken},
		"grant_type":    {"refresh_token"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", config.OAuthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed: %s", string(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	return &RefreshResult{
		AccessToken: result.AccessToken,
		ExpiresIn:   result.ExpiresIn,
	}, nil
}

// GetUserEmail gets the user email from an access token
func GetUserEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", config.OAuthUserInfoURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("user info request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		utils.Error("[OAuth] getUserEmail failed: %d %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("failed to get user info: %d", resp.StatusCode)
	}

	var userInfo struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return "", fmt.Errorf("failed to parse user info: %w", err)
	}

	return userInfo.Email, nil
}

// DiscoverProjectID discovers the project ID for the authenticated user
func DiscoverProjectID(ctx context.Context, accessToken string) (string, error) {
	var loadCodeAssistData map[string]interface{}

	for _, endpoint := range config.AntigravityEndpointFallbacks {
		projectID, data, err := tryDiscoverProject(ctx, accessToken, endpoint)
		if err != nil {
			utils.Warn("[OAuth] Project discovery failed at %s: %v", endpoint, err)
			continue
		}

		if projectID != "" {
			return projectID, nil
		}

		loadCodeAssistData = data
		// No project found - try to onboard
		utils.Info("[OAuth] No project in loadCodeAssist response, attempting onboardUser...")
		break
	}

	// Try onboarding if we got a response but no project
	if loadCodeAssistData != nil {
		tierId := getDefaultTierId(loadCodeAssistData)
		if tierId == "" {
			tierId = "FREE"
		}
		utils.Info("[OAuth] Onboarding user with tier: %s", tierId)

		onboardedProject, err := OnboardUser(ctx, accessToken, tierId, "", 10, 5000)
		if err == nil && onboardedProject != "" {
			utils.Success("[OAuth] Successfully onboarded, project: %s", onboardedProject)
			return onboardedProject, nil
		}
	}

	return "", nil
}

// tryDiscoverProject attempts to discover a project at a single endpoint
func tryDiscoverProject(ctx context.Context, accessToken, endpoint string) (string, map[string]interface{}, error) {
	reqBody := map[string]interface{}{
		"metadata": map[string]string{
			"ideType":    "IDE_UNSPECIFIED",
			"platform":   "PLATFORM_UNSPECIFIED",
			"pluginType": "GEMINI",
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/v1internal:loadCodeAssist", strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range config.LoadCodeAssistHeaders() {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("request failed with status %d", resp.StatusCode)
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", nil, err
	}

	// Check for project ID
	if projectID, ok := data["cloudaicompanionProject"].(string); ok && projectID != "" {
		return projectID, data, nil
	}

	if projectObj, ok := data["cloudaicompanionProject"].(map[string]interface{}); ok {
		if projectID, ok := projectObj["id"].(string); ok && projectID != "" {
			return projectID, data, nil
		}
	}

	return "", data, nil
}

// getDefaultTierId gets the default tier ID from loadCodeAssist response
func getDefaultTierId(data map[string]interface{}) string {
	allowedTiers, ok := data["allowedTiers"].([]interface{})
	if !ok || len(allowedTiers) == 0 {
		return ""
	}

	// Find the tier marked as default
	for _, tier := range allowedTiers {
		tierMap, ok := tier.(map[string]interface{})
		if !ok {
			continue
		}
		if isDefault, ok := tierMap["isDefault"].(bool); ok && isDefault {
			if id, ok := tierMap["id"].(string); ok {
				return id
			}
		}
	}

	// Fall back to first tier
	if firstTier, ok := allowedTiers[0].(map[string]interface{}); ok {
		if id, ok := firstTier["id"].(string); ok {
			return id
		}
	}

	return ""
}

// OAuthFlowResult represents the complete result of an OAuth flow
type OAuthFlowResult struct {
	Email        string
	RefreshToken string
	AccessToken  string
	ProjectID    string
}

// CompleteOAuthFlow completes the OAuth flow: exchange code and get all account info
func CompleteOAuthFlow(ctx context.Context, code, verifier string) (*OAuthFlowResult, error) {
	// Exchange code for tokens
	tokens, err := ExchangeCode(ctx, code, verifier)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}

	// Get user email
	email, err := GetUserEmail(ctx, tokens.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("failed to get user email: %w", err)
	}

	// Discover project ID
	projectID, _ := DiscoverProjectID(ctx, tokens.AccessToken)

	return &OAuthFlowResult{
		Email:        email,
		RefreshToken: tokens.RefreshToken,
		AccessToken:  tokens.AccessToken,
		ProjectID:    projectID,
	}, nil
}
