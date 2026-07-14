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

// fallbackChatSystemPrompt seeds a chat session when retrieval is unavailable
// and the user has not customized the chat prompt. The built-in default is
// written for RAG — it instructs the model to answer only from retrieved
// context, which without retrieval never exists — so left in place it would
// make the model refuse every question.
const fallbackChatSystemPrompt = "You are a helpful assistant."

// SystemPromptFor returns the system prompt seeding a chat session. A
// customized prompt is always honoured — configuration the user wrote is never
// silently overridden. Only the built-in default is swapped for a generic
// assistant prompt when retrieval is unavailable (see fallbackChatSystemPrompt).
//
// Customization is detected by comparing against the built-in default, which
// holds for both stores: the daemon store never persists an override equal to
// the default, and the client-local file loader fills unset fields from the
// defaults.
func SystemPromptFor(cfg PromptConfig, retrievalAvailable bool) string {
	if retrievalAvailable || cfg.ChatSystemPrompt != DefaultPrompts().ChatSystemPrompt {
		return cfg.ChatSystemPrompt
	}
	return fallbackChatSystemPrompt
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
