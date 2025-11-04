package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"dagger.io/dagger"
	"github.com/dagger/container-use/repository"
	"github.com/spf13/cobra"
)

var createCmd = &cobra.Command{
	Use:   "create [<title>]",
	Short: "Create a new containerized environment",
	Long: `Create a new development environment in a container.
The environment is created from a git reference (defaults to HEAD) and includes
the configured base image and setup commands.

The title describes the work that will be done in this environment. You can
provide it as a positional argument or via the --title flag.`,
	Args: cobra.MaximumNArgs(1),
	Example: `# Create environment with title as argument
container-use create "Fix authentication bug"

# Create from a specific branch
container-use create "Add new feature" --from-ref main

# Create with title as flag
container-use create --title "Refactor database layer"

# Create and output as JSON
container-use create "Update dependencies" --json`,
	RunE: func(app *cobra.Command, args []string) error {
		ctx := app.Context()

		// Resolve title from positional argument or flag
		title := ""
		if len(args) > 0 {
			title = args[0]
		}

		flagTitle, _ := app.Flags().GetString("title")
		if flagTitle != "" {
			title = flagTitle
		}

		if title == "" {
			return fmt.Errorf("title is required: provide it as an argument or use --title flag")
		}

		// Get flags
		fromRef, _ := app.Flags().GetString("from-ref")
		if fromRef == "" {
			fromRef = "HEAD"
		}

		jsonOutput, _ := app.Flags().GetBool("json")

		// Connect to Dagger
		slog.Info("connecting to dagger")

		dag, err := dagger.Connect(ctx, dagger.WithLogOutput(logWriter))
		if err != nil {
			slog.Error("Error starting dagger", "error", err)

			if isDockerDaemonError(err) {
				handleDockerDaemonError()
			}

			return fmt.Errorf("failed to connect to dagger: %w", err)
		}
		defer dag.Close()

		// Open repository
		repo, err := repository.Open(ctx, ".")
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}

		// Create environment
		slog.Info("creating environment", "title", title, "from_ref", fromRef)

		env, err := repo.Create(ctx, dag, title, "", fromRef)
		if err != nil {
			return fmt.Errorf("failed to create environment: %w", err)
		}

		// Check for uncommitted changes
		dirty, status, err := repo.IsDirty(ctx)
		if err != nil {
			return fmt.Errorf("unable to check if repository is dirty: %w", err)
		}

		// Output based on format
		if jsonOutput {
			// JSON output
			output := map[string]interface{}{
				"id":              env.ID,
				"title":           env.State.Title,
				"remote_ref":      fmt.Sprintf("container-use/%s", env.ID),
				"checkout_command": fmt.Sprintf("container-use checkout %s", env.ID),
				"log_command":     fmt.Sprintf("container-use log %s", env.ID),
				"diff_command":    fmt.Sprintf("container-use diff %s", env.ID),
				"config": map[string]interface{}{
					"base_image":       env.State.Config.BaseImage,
					"workdir":          env.State.Config.Workdir,
					"setup_commands":   env.State.Config.SetupCommands,
					"install_commands": env.State.Config.InstallCommands,
				},
			}

			if dirty {
				output["warning"] = "Repository has uncommitted changes that are NOT included in this environment"
				output["uncommitted_changes"] = status
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(output); err != nil {
				return fmt.Errorf("failed to encode JSON: %w", err)
			}

			return nil
		}

		// Standard output
		fmt.Printf("Environment created: %s\n", env.ID)
		fmt.Println()
		fmt.Println("Configuration:")
		fmt.Printf("  Base Image: %s\n", env.State.Config.BaseImage)
		fmt.Printf("  Workdir: %s\n", env.State.Config.Workdir)

		if len(env.State.Config.SetupCommands) > 0 {
			fmt.Printf("  Setup Commands: %d\n", len(env.State.Config.SetupCommands))
		}

		if len(env.State.Config.InstallCommands) > 0 {
			fmt.Printf("  Install Commands: %d\n", len(env.State.Config.InstallCommands))
		}

		envCount := len(env.State.Config.Env.Keys())
		if envCount > 0 {
			fmt.Printf("  Environment Variables: %d\n", envCount)
		}

		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Printf("  View logs:       container-use log %s\n", env.ID)
		fmt.Printf("  View changes:    container-use diff %s\n", env.ID)
		fmt.Printf("  Checkout branch: container-use checkout %s\n", env.ID)

		if dirty {
			fmt.Println()
			fmt.Printf("⚠️  WARNING: The repository has uncommitted changes that are NOT included in this environment.\n")
			fmt.Println("   The environment was created from the last committed state only.")
			fmt.Println()
			fmt.Println("Uncommitted changes detected:")
			fmt.Println(status)
			fmt.Println()
			fmt.Println("To include these changes, commit them first using git.")
		}

		return nil
	},
}

func init() {
	createCmd.Flags().StringP("title", "t", "", "Title describing the work in this environment")
	createCmd.Flags().StringP("from-ref", "r", "HEAD", "Git reference to create the environment from (branch, tag, or SHA)")
	createCmd.Flags().Bool("json", false, "Output result as JSON")

	rootCmd.AddCommand(createCmd)
}
