package basic

import (
	"fmt"
	"net/url"
	"os"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

const (
	groupID = "basic"

	// [chat] Inference snap API URLs
	openAi             = "openai"
	confOpenAiHttpHost = "chat.http.host"
	confOpenAiHttpPort = "chat.http.port"
	envOpenAiBasePath  = "OPENAI_BASE_PATH"

	// [knowledge] OpenSearch snap API URLs
	opensearch             = "opensearch"
	confOpenSearchHttpHost = "knowledge.http.host"
	confOpenSearchHttpPort = "knowledge.http.port"
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
func buildServiceURL(host any, port any, path string) string {
	u := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%v", host, port),
		Path:   path,
	}
	return u.String()
}

func serverApiUrls(ctx *common.Context) (map[string]string, error) {
	basePath, found := os.LookupEnv(envOpenAiBasePath)
	if !found {
		return nil, fmt.Errorf("%q env var is not set", envOpenAiBasePath)
	}

	openAIHost, err := getConfigValue(ctx, confOpenAiHttpHost)
	if err != nil {
		return nil, err
	}

	openAIPort, err := getConfigValue(ctx, confOpenAiHttpPort)
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

	return map[string]string{
		openAi:     buildServiceURL(openAIHost, openAIPort, basePath),
		opensearch: buildServiceURL(openSearchHost, openSearchPort, ""),
	}, nil
}
