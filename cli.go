package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

func runList() error {
	pkgs, err := getInstalledCtx(context.Background())
	if err != nil {
		return err
	}

	if jsonFlag {
		return printJSON(pkgs)
	}

	printPackageTable(
		[]string{"Name", "ID", "Version", "Source"},
		func(p Package) []string { return []string{p.Name, p.ID, p.Version, p.Source} },
		pkgs,
	)
	fmt.Printf("\n%d package(s) installed.\n", len(pkgs))
	return nil
}

func runCheck() error {
	pkgs, err := getUpgradeableCtx(context.Background())
	if err != nil {
		return err
	}

	if jsonFlag {
		if err := printJSON(pkgs); err != nil {
			return err
		}
	} else {
		if len(pkgs) == 0 {
			fmt.Println("All packages are up to date.")
		} else {
			printPackageTable(
				[]string{"Name", "ID", "Version", "Available"},
				func(p Package) []string { return []string{p.Name, p.ID, p.Version, p.Available} },
				pkgs,
			)
			fmt.Printf("\n%d package(s) have updates available.\n", len(pkgs))
		}
	}

	// Exit code 1 if updates exist, 0 if up to date.
	if len(pkgs) > 0 {
		os.Exit(1)
	}
	return nil
}

func printJSON(data interface{}) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func printPackageTable(headers []string, rowFn func(Package) []string, pkgs []Package) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, pkg := range pkgs {
		fmt.Fprintln(tw, strings.Join(rowFn(pkg), "\t"))
	}
	_ = tw.Flush()
}
