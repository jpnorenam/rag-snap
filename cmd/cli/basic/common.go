package basic

import (
	"fmt"
	"net/url"

	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
)

const (
	groupID = "basic"

	// [chat] Inference snap API URLs
	openAi             = "openai"
	confOpenAiHttpHost = "chat.http.host"
	confOpenAiHttpPort = "chat.http.port"
	confOpenAiHttpPath = "chat.http.path"

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

func serverApiUrls(ctx *common.Context) (map[string]string, error) {
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

	return map[string]string{
		openAi:     buildServiceURL(openAiHost, openAiPort, openAiBasePath, false),
		opensearch: buildServiceURL(openSearchHost, openSearchPort, "", true),
	}, nil
}
