package cli

import (
	"fmt"
	"git-exporter/internal/config"
	"git-exporter/internal/exporter"
	"sort"

	"github.com/apcera/termtables"
	"github.com/spf13/cobra"
)

// Version is set at build time using -ldflags "-X git-exporter/internal/cli.Version=...".
var Version = "dev"

// New builds the root command with native version flags and the export subcommand.
func New() *cobra.Command {
	root := &cobra.Command{Use: "git-exporter", Short: "Export Git Markdown files with their metadata", SilenceUsage: true, Version: Version}
	var filename, output string
	var repos []string
	var verbose bool
	fields := map[string]*[]string{}
	command := &cobra.Command{
		Use:   "export",
		Short: "Export configured projects",
		Long: `Export committed Markdown files and their Git metadata from one or more repositories.
Configure projects with --config (-c) or repeatable --repo name=source options.
Per-project options use name=value. Explicit CLI settings override configuration values.
Use --output (-o) to choose the destination directory.
Use --verbose (-v) to list exported files with their Page ID, Url, and Markdown file
path, followed by the total number of Markdown files retrieved.

SSH passphrases:
  DOCS_SSH_PASSPHRASE is an example environment variable containing the passphrase
  used to unlock an encrypted SSH private key. Set and export it in your shell
  before running the command, then select it with:
    --ssh-passphrase-env docs=DOCS_SSH_PASSPHRASE
  Here, docs is the project name; DOCS_SSH_PASSPHRASE is the variable name,
  not the passphrase itself. In YAML, use ssh_passphrase_env: DOCS_SSH_PASSPHRASE
  alongside ssh_key. The variable name is configurable and is not read automatically.
  When a key file is configured, a missing named variable produces an error.
  Omit this setting for an unencrypted key. If no key file is configured, the SSH
  agent is used and ssh_passphrase_env is ignored.`,
		Example: `  # Export projects from a configuration file, overriding the output directory.
  git-exporter export -c config.example.yaml -o ./export

  # Show exported document IDs, source URLs, file paths, and the total count.
  git-exporter export -c config.example.yaml -o ./export --verbose

  # Export a local repository without origin using an explicit file URL template.
  git-exporter export --repo docs=../my-project -o ./export \
    --site-url-template 'docs=https://git.example.com/team/project/blob/{commit}/{path}'

  # Export a public HTTPS repository at a specific branch or tag.
  git-exporter export --repo docs=https://github.com/organization/docs.git \
    --ref docs=main -o ./export

  # Export multiple repositories in one invocation.
  git-exporter export --repo docs=https://github.com/organization/docs.git \
    --repo wiki=https://github.com/organization/wiki.git -o ./export

  # Export over SSH with an encrypted private key (POSIX shell example).
  # Replace the placeholder with your key's passphrase.
  export DOCS_SSH_PASSPHRASE='<your SSH key passphrase>'
  git-exporter export --repo docs=git@github.com:organization/docs.git \
    --ssh-key docs=./keys/id_ed25519 \
    --ssh-passphrase-env docs=DOCS_SSH_PASSPHRASE -o ./export`,
		Args: cobra.NoArgs,
	}
	command.Flags().StringVarP(&filename, "config", "c", "", "YAML configuration file")
	command.Flags().StringVarP(&output, "output", "o", "", "Output directory")
	command.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show exported files in a table and their total count")
	command.Flags().StringArrayVar(&repos, "repo", nil, "Project name=source (repeatable)")
	for _, name := range []string{"ref", "username", "token-env", "ssh-key", "ssh-passphrase-env", "site-url-template"} {
		usage := "Setting name=value (repeatable)"
		if name == "ssh-passphrase-env" {
			usage = "Project name=ENV_VAR holding the SSH key passphrase (repeatable)"
		}
		fields[name] = command.Flags().StringArray(name, nil, usage)
	}
	command.RunE = func(cmd *cobra.Command, args []string) error {
		values := map[string][]string{}
		for k, v := range fields {
			values[k] = *v
		}
		cfg, err := config.Load(filename, config.Overrides{Output: output, OutputSet: cmd.Flags().Changed("output"), Repos: repos, Fields: values})
		if err != nil {
			return err
		}
		failures := 0
		var exportedFiles []exporter.ExportedFile
		for _, p := range cfg.Projects {
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			result, err := exporter.Run(cmd.Context(), cfg.Output, p, cmd.ErrOrStderr())
			if err != nil {
				failures++
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: error: %s\n", p.Name, exporter.SafeError(err, p))
				continue
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: mode=%s, exported=%d, deleted=%d\n", p.Name, result.Mode, result.Exported, result.Deleted)
			if verbose {
				exportedFiles = append(exportedFiles, result.Files...)
			}
		}
		if verbose {
			sort.Slice(exportedFiles, func(i, j int) bool { return exportedFiles[i].Path < exportedFiles[j].Path })
			table := termtables.CreateTable()
			table.AddHeaders("Page ID", "Url", "Markdown file")
			for _, file := range exportedFiles {
				table.AddRow(file.PageID, file.URL, file.Path)
			}
			fmt.Fprint(cmd.OutOrStdout(), table.Render())
			fmt.Fprintf(cmd.OutOrStdout(), "Markdown files retrieved: %d\n", len(exportedFiles))
		}
		if failures > 0 {
			return fmt.Errorf("%d project(s) failed", failures)
		}
		return nil
	}
	root.AddCommand(command)
	return root
}
