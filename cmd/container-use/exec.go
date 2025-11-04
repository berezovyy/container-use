package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"dagger.io/dagger"
	"github.com/dagger/container-use/repository"
	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:   "exec <env-id> <command>",
	Short: "Execute a command in an environment",
	Long: `Execute a single command in a containerized environment.

The command runs in the environment's container and any filesystem changes
are persisted to the environment's git branch. The output is displayed
when the command completes.

For interactive shell sessions, use 'container-use terminal' instead.`,
	Args: cobra.ExactArgs(2),
	Example: `# Execute a simple command
container-use exec adaptive-koala "ls -la"

# Run tests in an environment
container-use exec adaptive-koala "npm test"

# Execute with JSON output
container-use exec adaptive-koala "go build ./..." --json

# Use bash instead of default sh
container-use exec adaptive-koala "echo \$SHELL" --shell bash

# Use the container's entrypoint
container-use exec adaptive-koala "version" --use-entrypoint`,
	ValidArgsFunction: suggestEnvironments,
	RunE: func(app *cobra.Command, args []string) error {
		ctx := app.Context()

		envID := args[0]
		command := args[1]

		// Get flags
		jsonOutput, _ := app.Flags().GetBool("json")
		shell, _ := app.Flags().GetString("shell")
		useEntrypoint, _ := app.Flags().GetBool("use-entrypoint")

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

		// Load environment
		env, err := repo.Get(ctx, dag, envID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Environment '%s' not found.\n\n", envID)
			fmt.Fprintf(os.Stderr, "Run 'container-use list' to see available environments.\n")
			return fmt.Errorf("failed to load environment: %w", err)
		}

		// Execute command
		slog.Info("executing command", "env_id", envID, "command", command, "shell", shell)

		startTime := time.Now()
		stdout, stderr, exitCode, err := env.RunWithExitCode(ctx, command, shell, useEntrypoint)
		executionTime := time.Since(startTime)

		if err != nil {
			return fmt.Errorf("failed to execute command: %w", err)
		}

		// Update repository to persist changes
		slog.Info("updating repository")
		if updateErr := repo.Update(ctx, env, ""); updateErr != nil {
			slog.Error("failed to update repository", "error", updateErr)
			return fmt.Errorf("command executed but failed to update repository: %w", updateErr)
		}

		// Combine output
		output := stdout
		if stderr != "" {
			if stdout != "" {
				output += "\n"
			}
			output += "stderr: " + stderr
		}

		// Output based on format
		if jsonOutput {
			result := map[string]interface{}{
				"environment_id":    envID,
				"command":           command,
				"shell":             shell,
				"use_entrypoint":    useEntrypoint,
				"exit_code":         exitCode,
				"stdout":            stdout,
				"stderr":            stderr,
				"execution_time_ms": executionTime.Milliseconds(),
			}

			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(result); err != nil {
				return fmt.Errorf("failed to encode JSON: %w", err)
			}

			if exitCode != 0 {
				return fmt.Errorf("command exited with code %d", exitCode)
			}
			return nil
		}

		// Standard output
		if output != "" {
			fmt.Print(output)
			if output[len(output)-1] != '\n' {
				fmt.Println()
			}
		}

		if exitCode != 0 {
			fmt.Fprintf(os.Stderr, "\n❌ Command failed with exit code %d\n", exitCode)
			return fmt.Errorf("command exited with code %d", exitCode)
		}

		return nil
	},
}

func init() {
	execCmd.Flags().Bool("json", false, "Output result as JSON")
	execCmd.Flags().String("shell", "sh", "Shell to use for command execution")
	execCmd.Flags().Bool("use-entrypoint", false, "Use the container's entrypoint")

	rootCmd.AddCommand(execCmd)
}
