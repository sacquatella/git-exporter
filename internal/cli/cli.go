package cli

import (
	"fmt"
	"git-exporter/internal/config"
	"git-exporter/internal/exporter"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	root := &cobra.Command{Use: "git-exporter", Short: "Exporter les Markdown Git avec leurs métadonnées", SilenceUsage: true}
	var filename, output string
	var repos []string
	fields := map[string]*[]string{}
	command := &cobra.Command{Use: "export", Short: "Exporter les projets configurés", Args: cobra.NoArgs}
	command.Flags().StringVar(&filename, "config", "", "Fichier de configuration YAML")
	command.Flags().StringVar(&output, "output", "", "Répertoire cible")
	command.Flags().StringArrayVar(&repos, "repo", nil, "Projet nom=source (répétable)")
	for _, name := range []string{"ref", "username", "token-env", "ssh-key", "ssh-passphrase-env", "site-url-template"} {
		fields[name] = command.Flags().StringArray(name, nil, "Paramètre nom=valeur (répétable)")
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
		for _, p := range cfg.Projects {
			if err := cmd.Context().Err(); err != nil {
				return err
			}
			result, err := exporter.Run(cmd.Context(), cfg.Output, p, cmd.ErrOrStderr())
			if err != nil {
				failures++
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: erreur: %s\n", p.Name, exporter.SafeError(err, p))
				continue
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: mode=%s, exportés=%d, supprimés=%d\n", p.Name, result.Mode, result.Exported, result.Deleted)
		}
		if failures > 0 {
			return fmt.Errorf("%d projet(s) en échec", failures)
		}
		return nil
	}
	root.AddCommand(command)
	return root
}
