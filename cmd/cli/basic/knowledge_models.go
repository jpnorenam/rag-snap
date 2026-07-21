package basic

import (
	"context"
	"fmt"
	"sort"

	"github.com/jpnorenam/rag-snap/cmd/cli/basic/knowledge"
	"github.com/jpnorenam/rag-snap/cmd/cli/common"
	"github.com/jpnorenam/rag-snap/internal/apiclient"
	"github.com/spf13/cobra"
)

// modelsCommand groups the engine model inventory: what is registered, what is
// resident in memory, and how to reclaim the memory of models nothing uses.
// Models are never removed implicitly — `knowledge init` reuses what it finds and
// has no way to know whether a stray deployment is yours or a leftover.
func (cmd *knowledgeCommand) modelsCommand() *cobra.Command {
	cobraCmd := &cobra.Command{
		Use:   "models",
		Short: "List the engine's models and reclaim unused ones",
		Long: "List the models registered in the knowledge engine's model group, with their\n" +
			"deployment state, size, and the engine role they serve.\n\n" +
			"A deployed model is held in memory on every worker node, whether or not the\n" +
			"engine still refers to it. Models with no role are strays — from an interrupted\n" +
			"init, two inits running at once, or a model version that has since changed —\n" +
			"and 'models prune' removes them.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			models, err := cmd.engineModels(context.Background())
			if err != nil {
				return err
			}
			printModelInventory(models)
			return nil
		},
	}

	cobraCmd.AddCommand(
		cmd.modelsPruneCommand(),
		cmd.modelsRemoveCommand(),
	)

	return cobraCmd
}

func (cmd *knowledgeCommand) modelsPruneCommand() *cobra.Command {
	var yes bool

	cobraCmd := &cobra.Command{
		Use:   "prune",
		Short: "Undeploy and delete models the engine does not use",
		Long: "Remove every model in the engine's model group that no configuration key\n" +
			"points at, freeing the memory a deployed stray holds on the ML nodes.\n" +
			"The models in use for embedding and reranking are never touched.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx := context.Background()
			models, err := cmd.engineModels(ctx)
			if err != nil {
				return err
			}

			var strays []knowledge.ModelInfo
			for _, m := range models {
				if m.Role == "" {
					strays = append(strays, m)
				}
			}
			if len(strays) == 0 {
				fmt.Println("No unused models. Nothing to prune.")
				return nil
			}

			fmt.Printf("The following %d model(s) will be undeployed and deleted:\n\n", len(strays))
			printModelInventory(strays)
			if !yes && !common.ConfirmationPrompt("\nRemove them?") {
				return fmt.Errorf("prune aborted")
			}

			for _, m := range strays {
				if err := cmd.removeEngineModel(ctx, m.ID, false); err != nil {
					return fmt.Errorf("removing %s: %w", m.ID, err)
				}
				fmt.Printf("Removed %s (%s).\n", m.ID, m.Name)
			}
			return nil
		},
	}

	cobraCmd.Flags().BoolVarP(&yes, "yes", "y", false, "Do not ask for confirmation")

	return cobraCmd
}

func (cmd *knowledgeCommand) modelsRemoveCommand() *cobra.Command {
	var force bool

	cobraCmd := &cobra.Command{
		Use:   "remove <model_id>",
		Short: "Undeploy and delete a single model",
		Long: "Free a model's memory on the ML nodes and delete it.\n" +
			"A model the engine currently uses is refused unless --force is given:\n" +
			"removing it breaks ingest and search until 'knowledge init' runs again.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			if err := cmd.removeEngineModel(context.Background(), id, force); err != nil {
				return err
			}
			fmt.Printf("Removed %s.\n", id)
			return nil
		},
	}

	cobraCmd.Flags().BoolVarP(&force, "force", "f", false, "Remove the model even if the engine uses it")

	return cobraCmd
}

// engineModels returns the engine's model inventory through the daemon when one
// is running, or straight from OpenSearch otherwise.
func (cmd *knowledgeCommand) engineModels(ctx context.Context) ([]knowledge.ModelInfo, error) {
	if dc := daemonClient(cmd.Context); dc != nil {
		remote, err := dc.ListEngineModels(ctx)
		if err != nil {
			return nil, err
		}
		models := make([]knowledge.ModelInfo, 0, len(remote))
		for _, m := range remote {
			models = append(models, knowledge.ModelInfo(m))
		}
		return sortedModels(models), nil
	}

	client, err := cmd.opensearchClient()
	if err != nil {
		return nil, err
	}
	embedding, _ := getConfigString(cmd.Context, knowledge.ConfEmbeddingModelID)
	rerank, _ := getConfigString(cmd.Context, knowledge.ConfRerankModelID)

	models, err := client.ListModels(ctx, embedding, rerank)
	if err != nil {
		return nil, err
	}
	return sortedModels(models), nil
}

// removeEngineModel deletes a model through the daemon when one is running, or
// straight from OpenSearch otherwise. The in-use guard lives on both paths: the
// daemon enforces it for every client, and direct mode has no daemon to ask.
func (cmd *knowledgeCommand) removeEngineModel(ctx context.Context, id string, force bool) error {
	if dc := daemonClient(cmd.Context); dc != nil {
		return dc.DeleteEngineModel(ctx, id, force)
	}

	client, err := cmd.opensearchClient()
	if err != nil {
		return err
	}
	embedding, _ := getConfigString(cmd.Context, knowledge.ConfEmbeddingModelID)
	rerank, _ := getConfigString(cmd.Context, knowledge.ConfRerankModelID)

	if role := knowledge.ModelRole(id, embedding, rerank); role != "" && !force {
		return fmt.Errorf("model %s is the engine's %s model; pass --force to remove it anyway", id, role)
	}

	return client.DeleteModel(ctx, id)
}

// sortedModels orders the inventory so the models in use come first, then strays
// by name — the reading order of "what am I using, and what is left over".
func sortedModels(models []knowledge.ModelInfo) []knowledge.ModelInfo {
	sort.SliceStable(models, func(i, j int) bool {
		if (models[i].Role == "") != (models[j].Role == "") {
			return models[i].Role != ""
		}
		if models[i].Name != models[j].Name {
			return models[i].Name < models[j].Name
		}
		return models[i].Version < models[j].Version
	})
	return models
}

// printModelInventory renders the model table, matching the column style of the
// other knowledge listings.
func printModelInventory(models []knowledge.ModelInfo) {
	if len(models) == 0 {
		fmt.Println("No models registered. Run 'knowledge init' to set up the engine.")
		return
	}

	fmt.Printf("%-24s %-52s %-10s %-20s %-10s %-6s\n", "MODEL ID", "NAME", "VERSION", "STATE", "SIZE", "IN USE")
	for _, m := range models {
		role := m.Role
		if role == "" {
			role = "-"
		}
		fmt.Printf("%-24s %-52s %-10s %-20s %-10s %-6s\n",
			m.ID, m.Name, m.Version, m.State, modelSize(m.SizeBytes), role)
	}

	var strayBytes int64
	strays := 0
	for _, m := range models {
		if m.Role == "" && m.Deployed() {
			strays++
			strayBytes += m.SizeBytes
		}
	}
	if strays > 0 {
		fmt.Printf("\n%d unused model(s) are deployed, holding about %s per worker node.\n",
			strays, modelSize(strayBytes))
		fmt.Println("Run 'knowledge models prune' to undeploy and delete them.")
	}
}

// modelSize renders a model's size, or a placeholder when OpenSearch does not
// report one (a model registered from a URL may not carry the field).
func modelSize(n int64) string {
	if n <= 0 {
		return "-"
	}
	return humanBytes(n)
}

// compile-time check that the client view and the knowledge view stay
// convertible, so engineModels' conversion cannot silently lose a field.
var _ = knowledge.ModelInfo(apiclient.EngineModel{})
