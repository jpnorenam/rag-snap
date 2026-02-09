package basic

import (
	"fmt"
	"net/url"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/cmd/cli/config"
	"github.com/jpnorenam/rag-snap/pkg/storage"
	"github.com/spf13/cobra"
)

const (
	groupID = "basic"

	// [chat] Inference snap API URLs
	openAi             = "openai"
	confOpenAiHttpHost = "chat.http.host"
	confOpenAiHttpPort = "chat.http.port"
	confOpenAiHttpPath = "chat.http.path"
	confOpenAiHttpTLS  = "chat.http.tls"

	// [knowledge] OpenSearch snap API URLs
	opensearch             = "opensearch"
	confOpenSearchHttpHost = "knowledge.http.host"
	confOpenSearchHttpPort = "knowledge.http.port"
	confOpenSearchHttpTLS  = "knowledge.http.tls"

	// [tika] Tika snap API URLs
	tika             = "tika"
	confTikaHttpHost = "tika.http.host"
	confTikaHttpPort = "tika.http.port"
	confTikaHttpPath = "tika.http.path"
	confTikaHttpTLS  = "tika.http.tls"
)

func Group(title string) *cobra.Group {
	return &cobra.Group{
		ID:    groupID,
		Title: title,
	}
}

// getConfigString retrieves a single configuration value as a non-empty string.
func getConfigString(ctx *common.Context, key string) (string, error) {
	val, err := config.GetString(ctx.Config, key)
	if err != nil {
		return "", err
	}
	if val == "" {
		return "", fmt.Errorf("config key %q is not set", key)
	}
	return val, nil
}

// buildServiceURL constructs an HTTP URL from host, port, and optional path.
func buildServiceURL(host any, port any, path any, secure bool) string {
	u := url.URL{
		Host: fmt.Sprintf("%s:%v", host, port),
		Path: fmt.Sprintf("%v", path),
	}

	if secure {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}

	return u.String()
}

// addDebugFlags adds --debug and --config persistent flags to the command.
// When --debug is set, --config is required and the context's Config is
// replaced with a file-based read-only config parsed from the given path.
func addDebugFlags(cobraCmd *cobra.Command, ctx *common.Context) {
	var debug bool
	var configFile string

	cobraCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug mode (requires --config)")
	cobraCmd.PersistentFlags().MarkHidden("debug") // Todo: this isn't working, debug flag still shows up in help.

	cobraCmd.PersistentFlags().StringVar(&configFile, "config", "", "Path to config file (required with --debug)")
	cobraCmd.PersistentFlags().MarkHidden("config") // Todo: this isn't working, debug flag still shows up in help.

	original := cobraCmd.PersistentPreRunE
	cobraCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// Chain the root command's PersistentPreRunE (e.g. verbose flag handling).
		// We walk to the root instead of cmd.Parent() because for subcommands
		// (e.g. "knowledge list"), the parent IS this command, which would
		// cause infinite recursion.
		root := cmd
		for root.Parent() != nil {
			root = root.Parent()
		}
		if root.PersistentPreRunE != nil {
			if err := root.PersistentPreRunE(cmd, args); err != nil {
				return err
			}
		}
		// Chain any previously set PersistentPreRunE on this command
		if original != nil {
			if err := original(cmd, args); err != nil {
				return err
			}
		}

		if !debug {
			return nil
		}
		if configFile == "" {
			return fmt.Errorf("--config is required when --debug is enabled")
		}
		fileCfg, err := storage.NewFileConfig(configFile)
		if err != nil {
			return fmt.Errorf("loading config file: %w", err)
		}
		ctx.Config = fileCfg
		ctx.Debug = true
		return nil
	}
}

// getConfigBool retrieves a config value as a boolean.
// Returns the fallback when the key is unset or empty.
func getConfigBool(ctx *common.Context, key string, fallback bool) bool {
	val, err := config.GetString(ctx.Config, key)
	if err != nil || val == "" {
		return fallback
	}
	return val == "true" || val == "1"
}

func serverApiUrls(ctx *common.Context) (map[string]string, error) {
	openAiHost, err := getConfigString(ctx, confOpenAiHttpHost)
	if err != nil {
		return nil, err
	}
	openAiPort, err := getConfigString(ctx, confOpenAiHttpPort)
	if err != nil {
		return nil, err
	}
	openAiBasePath, err := getConfigString(ctx, confOpenAiHttpPath)
	if err != nil {
		return nil, err
	}
	openAiTLS := getConfigBool(ctx, confOpenAiHttpTLS, false)

	openSearchHost, err := getConfigString(ctx, confOpenSearchHttpHost)
	if err != nil {
		return nil, err
	}
	openSearchPort, err := getConfigString(ctx, confOpenSearchHttpPort)
	if err != nil {
		return nil, err
	}
	openSearchTLS := getConfigBool(ctx, confOpenSearchHttpTLS, true)

	tikaHost, err := getConfigString(ctx, confTikaHttpHost)
	if err != nil {
		return nil, err
	}
	tikaPort, err := getConfigString(ctx, confTikaHttpPort)
	if err != nil {
		return nil, err
	}
	tikaBasePath, err := getConfigString(ctx, confTikaHttpPath)
	if err != nil {
		return nil, err
	}
	tikaTLS := getConfigBool(ctx, confTikaHttpTLS, false)

	return map[string]string{
		openAi:     buildServiceURL(openAiHost, openAiPort, openAiBasePath, openAiTLS),
		opensearch: buildServiceURL(openSearchHost, openSearchPort, "", openSearchTLS),
		tika:       buildServiceURL(tikaHost, tikaPort, tikaBasePath, tikaTLS),
	}, nil
}
