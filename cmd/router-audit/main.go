package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/kavinbm16/mcp-dynamic-router/router"
)

func main() {
	path := flag.String("tools", "testdata/tools.json", "path to a JSON array of tools")
	minimum := flag.Int("minimum", 90, "minimum passing description score")
	flag.Parse()
	data, err := os.ReadFile(*path)
	fatalIf(err)
	var tools []router.Tool
	fatalIf(json.Unmarshal(data, &tools))
	failed := false
	for _, tool := range tools {
		report := router.AuditTool(tool, router.DefaultDescriptionPolicy())
		fmt.Printf("%s score=%d\n", tool.ID, report.Score)
		for _, issue := range report.Issues {
			fmt.Printf("  %s %s: %s\n", issue.Severity, issue.Code, issue.Message)
		}
		if report.Score < *minimum {
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
