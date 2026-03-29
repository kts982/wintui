package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var jsonOutput bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all installed packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		appSettings = LoadSettings()
		pkgs, err := getInstalledCtx(context.Background())
		if err != nil {
			return err
		}

		if jsonOutput {
			return printJSON(pkgs)
		}

		for _, p := range pkgs {
			fmt.Printf("%-40s %-30s %s\n", p.Name, p.ID, p.Version)
		}
		return nil
	},
}

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check for available upgrades",
	RunE: func(cmd *cobra.Command, args []string) error {
		appSettings = LoadSettings()
		pkgs, err := getUpgradeableCtx(context.Background())
		if err != nil {
			return err
		}

		if jsonOutput {
			return printJSON(pkgs)
		}

		if len(pkgs) == 0 {
			fmt.Println("All packages are up to date.")
			return nil
		}

		for _, p := range pkgs {
			fmt.Printf("%-40s %-30s %s -> %s\n", p.Name, p.ID, p.Version, p.Available)
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(checkCmd)
}

func printJSON(data interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
