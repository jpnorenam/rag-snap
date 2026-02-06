package basic

import (
	"fmt"
	"net/url"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
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
	// TODO add optional bearer token config keys
	// TODO add optional TLS enabled config keys

	// [knowledge] OpenSearch snap API URLs
	opensearch             = "opensearch"
	confOpenSearchHttpHost = "knowledge.http.host"
	confOpenSearchHttpPort = "knowledge.http.port"
	// TODO add optional TLS enabled config keys

	// [tika] Tika snap API URLs
	tika             = "tika"
	confTikaHttpHost = "tika.http.host"
	confTikaHttpPort = "tika.http.port"
	confTikaHttpPath = "tika.http.path"
	// TODO add optional TLS enabled config keys
)

func Group(title string) *cobra.Group {
	return &cobra.Group{
		ID:    groupID,
		Title: title,
	}
}

// getConfigValue retrieves a single configuration value by key.
func getConfigValue(ctx *common.Context, key string) (any, error) {
	configMap, err := ctx.Config.Get(key)
	if err != nil {
		return nil, fmt.Errorf("error getting %q: %v", key, err)
	}
	return configMap[key], nil
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
	cobraCmd.PersistentFlags().StringVar(&configFile, "config", "", "Path to config file (required with --debug)")

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

func serverApiUrls(ctx *common.Context) (map[string]string, error) {
	if ctx.Debug {
		return serverApiUrlsFromFile(ctx)
	}

	openAiBasePath, err := getConfigValue(ctx, confOpenAiHttpPath)
	if err != nil {
		return nil, err
	}

	openAiHost, err := getConfigValue(ctx, confOpenAiHttpHost)
	if err != nil {
		return nil, err
	}

	openAiPort, err := getConfigValue(ctx, confOpenAiHttpPort)
	if err != nil {
		return nil, err
	}

	openSearchHost, err := getConfigValue(ctx, confOpenSearchHttpHost)
	if err != nil {
		return nil, err
	}

	openSearchPort, err := getConfigValue(ctx, confOpenSearchHttpPort)
	if err != nil {
		return nil, err
	}

	tikaHost, err := getConfigValue(ctx, confTikaHttpHost)
	if err != nil {
		return nil, err
	}

	tikaPort, err := getConfigValue(ctx, confTikaHttpPort)
	if err != nil {
		return nil, err
	}

	tikeBasePath, err := getConfigValue(ctx, confTikaHttpPath)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		openAi:     buildServiceURL(openAiHost, openAiPort, openAiBasePath, false),
		opensearch: buildServiceURL(openSearchHost, openSearchPort, "", true),
		tika:       buildServiceURL(tikaHost, tikaPort, tikeBasePath, false),
	}, nil
}

// serverApiUrlsFromFile reads all config values at once from the debug config file.
func serverApiUrlsFromFile(ctx *common.Context) (map[string]string, error) {
	all, err := ctx.Config.GetAll()
	if err != nil {
		return nil, fmt.Errorf("reading debug config: %w", err)
	}

	required := []string{
		confOpenAiHttpHost, confOpenAiHttpPort, confOpenAiHttpPath,
		confOpenSearchHttpHost, confOpenSearchHttpPort,
		confTikaHttpHost, confTikaHttpPort, confTikaHttpPath,
	}
	for _, key := range required {
		if _, ok := all[key]; !ok {
			return nil, fmt.Errorf("missing required key %q in config file", key)
		}
	}

	return map[string]string{
		openAi:     buildServiceURL(all[confOpenAiHttpHost], all[confOpenAiHttpPort], all[confOpenAiHttpPath], false),
		opensearch: buildServiceURL(all[confOpenSearchHttpHost], all[confOpenSearchHttpPort], "", true),
		tika:       buildServiceURL(all[confTikaHttpHost], all[confTikaHttpPort], all[confTikaHttpPath], false),
	}, nil
}
