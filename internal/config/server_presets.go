// Package config provides server configuration presets management.
// This file corresponds to src/utils/server-presets.js in the Node.js version.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/poemonsense/antigravity-proxy-go/internal/utils"
)

// ServerPresetsManager handles reading and writing server config presets
type ServerPresetsManager struct {
	mu sync.RWMutex
}

// NewServerPresetsManager creates a new ServerPresetsManager
func NewServerPresetsManager() *ServerPresetsManager {
	return &ServerPresetsManager{}
}

// builtInNames returns a set of built-in preset names
func builtInNames() map[string]bool {
	names := make(map[string]bool)
	for _, p := range DefaultServerPresets {
		names[p.Name] = true
	}
	return names
}

// ReadServerPresets reads all server config presets.
// Creates the file with default presets if it doesn't exist.
func (m *ServerPresetsManager) ReadServerPresets() ([]ServerPreset, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.readPresetsInternal()
}

// readPresetsInternal reads presets without locking (for internal use)
func (m *ServerPresetsManager) readPresetsInternal() ([]ServerPreset, error) {
	presetsPath := ServerPresetsPath

	data, err := os.ReadFile(presetsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create file with defaults
			if writeErr := m.writePresetsInternal(DefaultServerPresets); writeErr != nil {
				utils.Warn("[ServerPresets] Could not create presets file: %v", writeErr)
			} else {
				utils.Info("[ServerPresets] Created presets file with defaults at %s", presetsPath)
			}
			return DefaultServerPresets, nil
		}
		utils.Error("[ServerPresets] Failed to read presets at %s: %v", presetsPath, err)
		return nil, err
	}

	if len(strings.TrimSpace(string(data))) == 0 {
		return DefaultServerPresets, nil
	}

	var userPresets []ServerPreset
	if err := json.Unmarshal(data, &userPresets); err != nil {
		utils.Error("[ServerPresets] Invalid JSON in presets at %s. Returning defaults.", presetsPath)
		return DefaultServerPresets, nil
	}

	// Merge: always include built-in presets (latest version), then user custom presets
	builtIn := builtInNames()
	var customPresets []ServerPreset
	for _, p := range userPresets {
		if !builtIn[p.Name] && !p.BuiltIn {
			customPresets = append(customPresets, p)
		}
	}

	result := make([]ServerPreset, 0, len(DefaultServerPresets)+len(customPresets))
	result = append(result, DefaultServerPresets...)
	result = append(result, customPresets...)

	return result, nil
}

// writePresetsInternal writes presets to file without locking
func (m *ServerPresetsManager) writePresetsInternal(presets []ServerPreset) error {
	presetsPath := ServerPresetsPath

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(presetsPath), 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(presetsPath, data, 0644)
}

// SaveServerPreset saves a custom server preset (add or update).
// Rejects overwriting built-in presets.
func (m *ServerPresetsManager) SaveServerPreset(name string, config ServerPresetConfig, description string) ([]ServerPreset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reject overwriting built-in presets
	builtIn := builtInNames()
	if builtIn[name] {
		return nil, errors.New("cannot overwrite built-in preset \"" + name + "\"")
	}

	allPresets, err := m.readPresetsInternal()
	if err != nil {
		return nil, err
	}

	// Find or create user custom preset
	existingIndex := -1
	for i, p := range allPresets {
		if p.Name == name && !p.BuiltIn {
			existingIndex = i
			break
		}
	}

	newPreset := ServerPreset{
		Name:   name,
		Config: config,
	}
	if strings.TrimSpace(description) != "" {
		newPreset.Description = strings.TrimSpace(description)
	}

	if existingIndex >= 0 {
		allPresets[existingIndex] = newPreset
		utils.Info("[ServerPresets] Updated preset: %s", name)
	} else {
		allPresets = append(allPresets, newPreset)
		utils.Info("[ServerPresets] Created preset: %s", name)
	}

	if err := m.writePresetsInternal(allPresets); err != nil {
		utils.Error("[ServerPresets] Failed to save preset: %v", err)
		return nil, err
	}

	return allPresets, nil
}

// UpdateServerPresetRequest represents the fields that can be updated
type UpdateServerPresetRequest struct {
	Name        *string             `json:"name,omitempty"`
	Description *string             `json:"description,omitempty"`
	Config      *ServerPresetConfig `json:"config,omitempty"`
}

// UpdateServerPreset updates metadata (name, description) of a custom server preset.
// Rejects editing built-in presets.
func (m *ServerPresetsManager) UpdateServerPreset(currentName string, updates UpdateServerPresetRequest) ([]ServerPreset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	builtIn := builtInNames()
	if builtIn[currentName] {
		return nil, errors.New("cannot edit built-in preset \"" + currentName + "\"")
	}

	allPresets, err := m.readPresetsInternal()
	if err != nil {
		return nil, err
	}

	index := -1
	for i, p := range allPresets {
		if p.Name == currentName && !p.BuiltIn {
			index = i
			break
		}
	}

	if index < 0 {
		return nil, errors.New("preset \"" + currentName + "\" not found")
	}

	// Check new name doesn't collide with a built-in
	if updates.Name != nil && builtIn[*updates.Name] {
		return nil, errors.New("cannot use built-in preset name \"" + *updates.Name + "\"")
	}

	// Check new name doesn't collide with another custom preset
	if updates.Name != nil && *updates.Name != currentName {
		for i, p := range allPresets {
			if p.Name == *updates.Name && !p.BuiltIn && i != index {
				return nil, errors.New("a preset named \"" + *updates.Name + "\" already exists")
			}
		}
	}

	if updates.Name != nil {
		allPresets[index].Name = strings.TrimSpace(*updates.Name)
	}

	if updates.Description != nil {
		trimmed := strings.TrimSpace(*updates.Description)
		if trimmed != "" {
			allPresets[index].Description = trimmed
		} else {
			allPresets[index].Description = ""
		}
	}

	// Merge config updates if provided
	if updates.Config != nil {
		allPresets[index].Config = *updates.Config
	}

	hasConfigChange := updates.Config != nil
	hasNameChange := updates.Name != nil && *updates.Name != currentName

	if err := m.writePresetsInternal(allPresets); err != nil {
		utils.Error("[ServerPresets] Failed to update preset: %v", err)
		return nil, err
	}

	changeType := "metadata"
	if hasConfigChange {
		changeType = "config"
	}
	nameChange := ""
	if hasNameChange {
		nameChange = " → " + *updates.Name
	}
	utils.Info("[ServerPresets] Updated preset %s: %s%s", changeType, currentName, nameChange)

	return allPresets, nil
}

// DeleteServerPreset deletes a custom server preset by name.
// Rejects deletion of built-in presets.
func (m *ServerPresetsManager) DeleteServerPreset(name string) ([]ServerPreset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	builtIn := builtInNames()
	if builtIn[name] {
		return nil, errors.New("cannot delete built-in preset \"" + name + "\"")
	}

	allPresets, err := m.readPresetsInternal()
	if err != nil {
		return nil, err
	}

	originalLength := len(allPresets)
	filtered := make([]ServerPreset, 0, originalLength)
	for _, p := range allPresets {
		if p.Name != name {
			filtered = append(filtered, p)
		}
	}

	if len(filtered) == originalLength {
		utils.Warn("[ServerPresets] Preset not found: %s", name)
		return filtered, nil
	}

	if err := m.writePresetsInternal(filtered); err != nil {
		utils.Error("[ServerPresets] Failed to delete preset: %v", err)
		return nil, err
	}

	utils.Info("[ServerPresets] Deleted preset: %s", name)
	return filtered, nil
}

// Global instance
var (
	globalPresetsManager     *ServerPresetsManager
	globalPresetsManagerOnce sync.Once
)

// GetServerPresetsManager returns the global ServerPresetsManager instance
func GetServerPresetsManager() *ServerPresetsManager {
	globalPresetsManagerOnce.Do(func() {
		globalPresetsManager = NewServerPresetsManager()
	})
	return globalPresetsManager
}
