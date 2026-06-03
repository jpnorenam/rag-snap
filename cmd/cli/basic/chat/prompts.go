package chat

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const promptsConfigFile = "prompts.json"

// PromptConfig holds the three configurable system prompts for the RAG pipeline.
type PromptConfig struct {
	SourceRules        string `json:"source_rules"`
	AnswerSystemPrompt string `json:"answer_system_prompt"`
	ChatSystemPrompt   string `json:"chat_system_prompt"`
}

// DefaultPrompts returns the built-in default prompt configuration.
func DefaultPrompts() PromptConfig {
	return PromptConfig{
		SourceRules:        ragSourceRules,
		AnswerSystemPrompt: ragAnswerSystemPrompt,
		ChatSystemPrompt:   ragChatSystemPrompt,
	}
}

// promptsConfigPath returns the path to the prompts config JSON file.
func promptsConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "rag-cli", promptsConfigFile), nil
}

// LoadPrompts reads custom prompts from disk. If the file does not exist or
// cannot be parsed, the built-in defaults are returned. Empty fields in a
// partial config are filled in with their defaults.
func LoadPrompts() PromptConfig {
	defaults := DefaultPrompts()

	path, err := promptsConfigPath()
	if err != nil {
		return defaults
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaults
		}
		return defaults
	}

	var cfg PromptConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return defaults
	}

	if cfg.SourceRules == "" {
		cfg.SourceRules = defaults.SourceRules
	}
	if cfg.AnswerSystemPrompt == "" {
		cfg.AnswerSystemPrompt = defaults.AnswerSystemPrompt
	}
	if cfg.ChatSystemPrompt == "" {
		cfg.ChatSystemPrompt = defaults.ChatSystemPrompt
	}

	return cfg
}

// SavePrompts writes cfg to the prompts config file, creating the directory if needed.
func SavePrompts(cfg PromptConfig) error {
	path, err := promptsConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
