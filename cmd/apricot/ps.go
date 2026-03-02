package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ieee0824/apricot/internal/runner"
)

func runPs(args []string) {
	fs := flag.NewFlagSet("ps", flag.ExitOnError)
	project := fs.String("p", "", "Project name (default: current directory name)")
	all := fs.Bool("a", false, "Show all containers including stopped")
	fs.Parse(args)

	projectName := resolveProjectName(*project)

	containers, err := runner.List(*all)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prefix := projectName + "-"
	fmt.Printf("%-40s %-30s %-12s\n", "NAME", "IMAGE", "STATE")
	fmt.Println(strings.Repeat("-", 84))
	for _, c := range containers {
		if strings.HasPrefix(c.Name, prefix) {
			fmt.Printf("%-40s %-30s %-12s\n", c.Name, c.Image, c.State)
		}
	}
}
