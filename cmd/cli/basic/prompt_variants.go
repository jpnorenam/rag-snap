package basic

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/jpnorenam/rag-snap/internal/apiclient"
	"github.com/spf13/cobra"
)

// generationSlots are the two prompt slots that support named variants (the
// source_rules guardrail does not).
var generationSlots = []string{string(keyChatSystemPrompt), string(keyAnswerSystemPrompt)}

// allSlots lists every valid slot name, for argument validation and completion.
var allSlots = []string{
	string(keyChatSystemPrompt),
	string(keyAnswerSystemPrompt),
	string(keySourceRules),
}

// requireDaemon returns a daemon client or an error telling the user to start
// the daemon. The variant subcommands are daemon-only: the client-local prompts
// file has no notion of variants.
func requireDaemon(c *promptCommand) (*apiclient.Client, error) {
	if dc := daemonClient(c.Context); dc != nil {
		return dc, nil
	}
	return nil, fmt.Errorf("this command manages daemon-owned prompt variants and needs the ragd daemon; start it and retry")
}

// validSlot reports whether name is one of the three slots.
func validSlot(name string) bool {
	for _, s := range allSlots {
		if s == name {
			return true
		}
	}
	return false
}

// isGenerationSlot reports whether the slot supports variants.
func isGenerationSlot(name string) bool {
	for _, s := range generationSlots {
		if s == name {
			return true
		}
	}
	return false
}

// requireVariantSlot validates a slot argument for a variant subcommand.
func requireVariantSlot(name string) error {
	if !validSlot(name) {
		return fmt.Errorf("unknown prompt %q; valid prompts are: %s", name, strings.Join(allSlots, ", "))
	}
	if !isGenerationSlot(name) {
		return fmt.Errorf("%q does not support variants (it is the grounding guardrail); it has only a single override", name)
	}
	return nil
}

// slotCompletions offers slot names for shell completion.
func slotCompletions(slots []string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return slots, cobra.ShellCompDirectiveNoFileComp
	}
}

// listCommand prints every slot with its active selection and, for generation
// slots, its stored variants.
func (cmd *promptCommand) listCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List prompt slots and their variants",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			dc, err := requireDaemon(cmd)
			if err != nil {
				return err
			}
			ctx := context.Background()

			prompts, err := dc.ListPrompts(ctx)
			if err != nil {
				return fmt.Errorf("reading prompts from the daemon: %w", err)
			}
			for _, p := range prompts {
				state := "default"
				if p.Customized {
					if p.Active != "" {
						state = "active variant: " + p.Active
					} else {
						state = "customized"
					}
				}
				fmt.Printf("%s  [%s]\n", p.Name, state)

				if !isGenerationSlot(p.Name) {
					continue
				}
				variants, err := dc.ListPromptVariants(ctx, p.Name)
				if err != nil {
					return fmt.Errorf("listing variants of %s: %w", p.Name, err)
				}
				if len(variants) == 0 {
					fmt.Println("    (no variants)")
					continue
				}
				for _, v := range variants {
					marker := "  "
					if v.Active {
						marker = "* "
					}
					fmt.Printf("    %s%s  (%d version(s))\n", marker, v.Name, v.Versions)
				}
			}
			return nil
		},
	}
}

// saveCommand creates or appends a version to a variant, reading the value from
// a file or stdin.
func (cmd *promptCommand) saveCommand() *cobra.Command {
	var file string
	c := &cobra.Command{
		Use:               "save <slot> <name>",
		Short:             "Save a prompt variant (new version) from a file or stdin",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: slotCompletions(generationSlots),
		RunE: func(_ *cobra.Command, args []string) error {
			slot, name := args[0], args[1]
			if err := requireVariantSlot(slot); err != nil {
				return err
			}
			dc, err := requireDaemon(cmd)
			if err != nil {
				return err
			}
			value, err := readPromptValue(file)
			if err != nil {
				return err
			}
			v, err := dc.SavePromptVariant(context.Background(), slot, name, value)
			if err != nil {
				return fmt.Errorf("saving variant: %w", err)
			}
			fmt.Printf("Saved %s/%s (now at version %d).\n", slot, name, v.Version)
			fmt.Printf("Activate it with: %s prompt use %s %s\n", cliCommand(), slot, name)
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "Read the prompt from this file (default: stdin)")
	return c
}

// useCommand activates a variant, or the built-in default with --default.
func (cmd *promptCommand) useCommand() *cobra.Command {
	var useDefault bool
	c := &cobra.Command{
		Use:               "use <slot> [name]",
		Short:             "Activate a prompt variant (or --default) on a slot",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: slotCompletions(generationSlots),
		RunE: func(_ *cobra.Command, args []string) error {
			slot := args[0]
			if err := requireVariantSlot(slot); err != nil {
				return err
			}
			var name string
			switch {
			case useDefault:
				if len(args) == 2 {
					return fmt.Errorf("provide either a variant name or --default, not both")
				}
			case len(args) == 2:
				name = args[1]
			default:
				return fmt.Errorf("provide a variant name, or --default to return to the built-in default")
			}
			dc, err := requireDaemon(cmd)
			if err != nil {
				return err
			}
			if _, err := dc.ActivatePrompt(context.Background(), slot, name); err != nil {
				return fmt.Errorf("activating: %w", err)
			}
			if name == "" {
				fmt.Printf("%s is back to its built-in default. New chats and batch runs will use it.\n", slot)
			} else {
				fmt.Printf("%s is now using variant %q. New chats and batch runs will use it.\n", slot, name)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&useDefault, "default", false, "Activate the built-in default instead of a variant")
	return c
}

// historyCommand prints a variant's version history with a short preview.
func (cmd *promptCommand) historyCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "history <slot> <name>",
		Short:             "Show a variant's version history",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: slotCompletions(generationSlots),
		RunE: func(_ *cobra.Command, args []string) error {
			slot, name := args[0], args[1]
			if err := requireVariantSlot(slot); err != nil {
				return err
			}
			dc, err := requireDaemon(cmd)
			if err != nil {
				return err
			}
			versions, err := dc.PromptVariantVersions(context.Background(), slot, name)
			if err != nil {
				return fmt.Errorf("reading history: %w", err)
			}
			for i := len(versions) - 1; i >= 0; i-- {
				v := versions[i]
				marker := "  "
				if i == len(versions)-1 {
					marker = "* " // head
				}
				fmt.Printf("%sv%d: %s\n", marker, v.Version, previewLine(v.Value))
			}
			return nil
		},
	}
}

// restoreCommand appends an earlier version's content as a new head.
func (cmd *promptCommand) restoreCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "restore <slot> <name> <version>",
		Short:             "Restore an earlier version of a variant (as a new version)",
		Args:              cobra.ExactArgs(3),
		ValidArgsFunction: slotCompletions(generationSlots),
		RunE: func(_ *cobra.Command, args []string) error {
			slot, name := args[0], args[1]
			if err := requireVariantSlot(slot); err != nil {
				return err
			}
			version, err := strconv.Atoi(args[2])
			if err != nil {
				return fmt.Errorf("version must be a number, got %q", args[2])
			}
			dc, err := requireDaemon(cmd)
			if err != nil {
				return err
			}
			v, err := dc.RestorePromptVariant(context.Background(), slot, name, version)
			if err != nil {
				return fmt.Errorf("restoring: %w", err)
			}
			fmt.Printf("Restored %s/%s version %d as the new head (version %d).\n", slot, name, version, v.Version)
			return nil
		},
	}
}

// deleteCommand removes a variant.
func (cmd *promptCommand) deleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "delete <slot> <name>",
		Short:             "Delete a prompt variant",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: slotCompletions(generationSlots),
		RunE: func(_ *cobra.Command, args []string) error {
			slot, name := args[0], args[1]
			if err := requireVariantSlot(slot); err != nil {
				return err
			}
			dc, err := requireDaemon(cmd)
			if err != nil {
				return err
			}
			if err := dc.DeletePromptVariant(context.Background(), slot, name); err != nil {
				return fmt.Errorf("deleting variant: %w", err)
			}
			fmt.Printf("Deleted %s/%s.\n", slot, name)
			return nil
		},
	}
}

// readPromptValue reads a prompt value from the named file, or from stdin when
// the path is empty or "-".
func readPromptValue(path string) (string, error) {
	if path == "" || path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading prompt from stdin: %w", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return "", fmt.Errorf("no prompt text supplied on stdin; pass --file or pipe the text in")
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading prompt file: %w", err)
	}
	return string(data), nil
}

// previewLine collapses a prompt to a single short line for listings.
func previewLine(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	const maxLen = 72
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}

// runVariantInit is the interactive `prompt init` flow for a generation slot: it
// offers the stored variants, a "create new" option, and the built-in default,
// then edits / activates / restores the chosen selection. It is only reached
// when a daemon is available.
func runVariantInit(dc *apiclient.Client, slot promptKey) error {
	ctx := context.Background()
	slotName := string(slot)

	variants, err := dc.ListPromptVariants(ctx, slotName)
	if err != nil {
		return fmt.Errorf("listing variants: %w", err)
	}

	const (
		optNew     = "\x00new"
		optDefault = "\x00default"
	)
	options := make([]huh.Option[string], 0, len(variants)+2)
	for _, v := range variants {
		label := v.Name
		if v.Active {
			label += "  [active]"
		}
		options = append(options, huh.NewOption(label, v.Name))
	}
	options = append(options,
		huh.NewOption("＋ Create a new variant", optNew),
		huh.NewOption("Use the built-in default", optDefault),
	)

	var choice string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(fmt.Sprintf("Configure %s", slotName)).
			Options(options...).
			Value(&choice),
	))
	if ok, err := runForm(form); err != nil || !ok {
		return err
	}

	switch choice {
	case optDefault:
		if _, err := dc.ActivatePrompt(ctx, slotName, ""); err != nil {
			return fmt.Errorf("activating the default: %w", err)
		}
		fmt.Printf("%s is back to its built-in default. New chats and batch runs will use it.\n", slotName)
		return nil
	case optNew:
		return createVariantInteractive(ctx, dc, slotName)
	default:
		return editVariantInteractive(ctx, dc, slotName, choice)
	}
}

// createVariantInteractive prompts for a name and initial value, creates the
// variant, and offers to activate it.
func createVariantInteractive(ctx context.Context, dc *apiclient.Client, slot string) error {
	var name string
	nameForm := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("New variant name").
			Description("Lowercase letters, digits and hyphens (e.g. presales-call).").
			Value(&name),
	))
	if ok, err := runForm(nameForm); err != nil || !ok {
		return err
	}
	name = strings.TrimSpace(name)

	value, ok, err := editPrompt(promptKey(slot+"/"+name), "")
	if err != nil || !ok {
		return err
	}
	if _, err := dc.CreatePromptVariant(ctx, slot, name, value); err != nil {
		return fmt.Errorf("creating variant: %w", err)
	}
	fmt.Printf("Created %s/%s.\n", slot, name)

	activate, ok, err := confirmActivate(name)
	if err != nil || !ok {
		return err
	}
	if activate {
		if _, err := dc.ActivatePrompt(ctx, slot, name); err != nil {
			return fmt.Errorf("activating: %w", err)
		}
		fmt.Printf("%s is now using variant %q. New chats and batch runs will use it.\n", slot, name)
	}
	return nil
}

// editVariantInteractive offers the actions available on an existing variant.
func editVariantInteractive(ctx context.Context, dc *apiclient.Client, slot, name string) error {
	const (
		actEdit     = "edit"
		actActivate = "activate"
		actRestore  = "restore"
		actDelete   = "delete"
	)
	var action string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title(fmt.Sprintf("%s/%s — what do you want to do?", slot, name)).
			Options(
				huh.NewOption("Edit (save a new version)", actEdit),
				huh.NewOption("Activate", actActivate),
				huh.NewOption("Restore an earlier version", actRestore),
				huh.NewOption("Delete", actDelete),
			).
			Value(&action),
	))
	if ok, err := runForm(form); err != nil || !ok {
		return err
	}

	switch action {
	case actEdit:
		current, err := dc.GetPromptVariant(ctx, slot, name)
		if err != nil {
			return fmt.Errorf("reading variant: %w", err)
		}
		edited, ok, err := editPrompt(promptKey(slot+"/"+name), current.Value)
		if err != nil || !ok {
			return err
		}
		if edited == current.Value {
			fmt.Println("No changes made.")
			return nil
		}
		v, err := dc.SavePromptVariant(ctx, slot, name, edited)
		if err != nil {
			return fmt.Errorf("saving variant: %w", err)
		}
		fmt.Printf("Saved %s/%s (now at version %d).\n", slot, name, v.Version)
		return nil
	case actActivate:
		if _, err := dc.ActivatePrompt(ctx, slot, name); err != nil {
			return fmt.Errorf("activating: %w", err)
		}
		fmt.Printf("%s is now using variant %q. New chats and batch runs will use it.\n", slot, name)
		return nil
	case actRestore:
		return restoreVariantInteractive(ctx, dc, slot, name)
	case actDelete:
		if err := dc.DeletePromptVariant(ctx, slot, name); err != nil {
			return fmt.Errorf("deleting variant: %w", err)
		}
		fmt.Printf("Deleted %s/%s.\n", slot, name)
		return nil
	}
	return nil
}

// restoreVariantInteractive lets the user pick an earlier version to restore.
func restoreVariantInteractive(ctx context.Context, dc *apiclient.Client, slot, name string) error {
	versions, err := dc.PromptVariantVersions(ctx, slot, name)
	if err != nil {
		return fmt.Errorf("reading history: %w", err)
	}
	if len(versions) < 2 {
		fmt.Println("This variant has no earlier versions to restore.")
		return nil
	}
	options := make([]huh.Option[int], 0, len(versions))
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		options = append(options, huh.NewOption(fmt.Sprintf("v%d: %s", v.Version, previewLine(v.Value)), v.Version))
	}
	var version int
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[int]().
			Title("Restore which version?").
			Options(options...).
			Value(&version),
	))
	if ok, err := runForm(form); err != nil || !ok {
		return err
	}
	v, err := dc.RestorePromptVariant(ctx, slot, name, version)
	if err != nil {
		return fmt.Errorf("restoring: %w", err)
	}
	fmt.Printf("Restored version %d as the new head (version %d).\n", version, v.Version)
	return nil
}

// confirmActivate asks whether to activate a freshly created variant.
func confirmActivate(name string) (activate bool, ok bool, err error) {
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Activate %q now?", name)).
			Description("New chats and batch runs will use it.").
			Value(&activate),
	))
	ok, err = runForm(form)
	return activate, ok, err
}
