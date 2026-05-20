// internal/cmd/validate.go
package cmd

import (
	"fmt"
	"os"

	"github.com/phildougherty/m8e/internal/config"

	"github.com/spf13/cobra"
)

func NewValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate the compose file",
		Long: `Validate the matey.yaml configuration file.

Reports every problem found at once, each pointing at the source line.

The file to validate is, in precedence order:
  1. a positional argument:   matey validate ./other.yaml
  2. the persistent --file/-c flag:  matey -c other.yaml validate
  3. the default 'matey.yaml' in the current directory.

Use --emit-schema to write a JSON Schema for the config file, which can be
wired into editors (VS Code YAML, JetBrains) for autocomplete and inline
validation of matey.yaml. Pass --emit-schema=- to write the schema to stdout.`,
		Args: cobra.MaximumNArgs(1),
		// Validation failures are user errors, not usage errors — don't dump
		// the help text on top of the located problem list.
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Positional path wins over the persistent --file flag — kubectl
			// convention. Operators reach for `matey validate path.yaml`
			// first; do the obvious thing.
			file, _ := cmd.Flags().GetString("file")
			if len(args) == 1 {
				file = args[0]
			}
			emitSchema, _ := cmd.Flags().GetString("emit-schema")

			if cmd.Flags().Changed("emit-schema") {

				return emitJSONSchema(cmd, emitSchema)
			}

			cfg, err := config.ValidateFile(file)
			if err != nil {
				if verrs, ok := err.(config.ValidationErrors); ok {
					fmt.Fprintf(os.Stderr, "%s is invalid (%d problem(s)):\n", file, len(verrs))
					for _, ve := range verrs {
						fmt.Fprintf(os.Stderr, "  %s\n", ve.Error())
					}

					return fmt.Errorf("configuration validation failed")
				}

				return err
			}

			fmt.Printf("%s is valid (%d server(s))\n", file, len(cfg.Servers))

			return nil
		},
	}

	cmd.Flags().String("emit-schema", "", "Write a JSON Schema for matey.yaml to the given path (use '-' for stdout)")
	cmd.Flags().Lookup("emit-schema").NoOptDefVal = "matey.schema.json"

	return cmd
}

func emitJSONSchema(cmd *cobra.Command, path string) error {
	data, err := config.GenerateSchemaJSON()
	if err != nil {

		return fmt.Errorf("failed to generate schema: %w", err)
	}

	if path == "" || path == "-" {
		fmt.Fprintln(cmd.OutOrStdout(), string(data))

		return nil
	}

	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {

		return fmt.Errorf("failed to write schema to %s: %w", path, err)
	}
	fmt.Printf("Wrote JSON Schema to %s\n", path)

	return nil
}
