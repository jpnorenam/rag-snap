package basic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/canonical/go-snapctl"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/chat"
	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type statusCommand struct {
	*common.Context

	// flags
	format string
}

func StatusCommand(ctx *common.Context) *cobra.Command {
	var cmd statusCommand
	cmd.Context = ctx

	cobraCmd := &cobra.Command{
		Use:               "status",
		Short:             "Show the status",
		Long:              "Show the status of the inference snap",
		GroupID:           groupID,
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE:              cmd.run,
	}

	// flags
	cobraCmd.Flags().StringVar(&cmd.format, "format", "yaml", "output format")

	return cobraCmd
}

func (cmd *statusCommand) run(_ *cobra.Command, _ []string) error {
	var statusText string
	var err error

	stopProgress := common.StartProgressSpinner("Getting status")
	defer stopProgress()

	switch cmd.format {
	case "json":
		statusText, err = cmd.statusJson()
		if err != nil {
			return fmt.Errorf("error getting json status: %w", err)
		}
		statusText += "\n"
	case "yaml":
		statusText, err = cmd.statusYaml()
		if err != nil {
			return fmt.Errorf("error getting yaml status: %w", err)
		}
	default:
		return fmt.Errorf("unknown format %q", cmd.format)
	}

	stopProgress()

	fmt.Print(statusText)

	return nil
}

func (cmd *statusCommand) statusYaml() (string, error) {
	statusStr, err := cmd.statusStruct()
	if err != nil {
		return "", fmt.Errorf("error getting status: %w", err)
	}
	yamlStr, err := yaml.Marshal(statusStr)
	if err != nil {
		return "", fmt.Errorf("error marshalling yaml: %w", err)
	}
	return string(yamlStr), nil
}

func (cmd *statusCommand) statusJson() (string, error) {
	statusStr, err := cmd.statusStruct()
	if err != nil {
		return "", fmt.Errorf("error getting status: %w", err)
	}
	jsonStr, err := json.MarshalIndent(statusStr, "", "  ")
	if err != nil {
		return "", fmt.Errorf("error marshalling json: %w", err)
	}
	return string(jsonStr), nil
}

type Status struct {
	Models    map[string]string `json:"models" yaml:"models"`
	Services  map[string]string `json:"services" yaml:"services"`
	Endpoints map[string]string `json:"endpoints" yaml:"endpoints"`
}

func (cmd *statusCommand) statusStruct() (*Status, error) {
	var statusStr Status

	services, err := snapctl.Services().Run()
	if err != nil {
		return nil, fmt.Errorf("error getting services: %w", err)
	}
	statusStr.Services = make(map[string]string)
	for name, service := range services {
		// The service name is in the format <snap-name>.<service-app>, we only want the service-app part.
		_, serviceApp, found := strings.Cut(name, ".")
		if !found {
			return nil, fmt.Errorf("error unexpected service name format: %q", name)
		}
		// Append the service status exactly as snapd reports it. Often this is in the host system language, see bug:
		// https://bugs.launchpad.net/snapd/+bug/2137543
		statusStr.Services[serviceApp] = service.Current
	}

	endpoints, err := serverApiUrls(cmd.Context)
	if err != nil {
		return nil, fmt.Errorf("error getting server api endpoints: %w", err)
	}
	statusStr.Endpoints = endpoints

	// Populate model information
	statusStr.Models = make(map[string]string)

	// LLM model name from the inference server (best-effort)
	if llmName, err := chat.FindModelName(endpoints[openAi]); err == nil {
		statusStr.Models["llm"] = llmName
	}

	// Embedding model name and OpenSearch model ID
	embeddingModelID, _ := getConfigString(cmd.Context, knowledge.ConfEmbeddingModelID)
	if embeddingModelID != "" {
		statusStr.Models["embedding"] = fmt.Sprintf("%s (%s)", knowledge.DefaultSentenceTransformerName, embeddingModelID)
	}

	// Reranker model name and OpenSearch model ID
	rerankModelID, _ := getConfigString(cmd.Context, knowledge.ConfRerankModelID)
	if rerankModelID != "" {
		statusStr.Models["reranker"] = fmt.Sprintf("%s (%s)", knowledge.DefaultCrossEncoderName, rerankModelID)
	}

	return &statusStr, nil
}
