package main

import (
	"flag"
	"fmt"
	"os"
	"plansql/internal/cli"
	"plansql/internal/mcp"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "mcp":
			fs := flag.NewFlagSet("mcp", flag.ExitOnError)
			host := fs.String("host", "", "daemon host")
			_ = fs.Parse(os.Args[2:])
			if err := mcp.RunMCPServer(*host); err != nil {
				fmt.Fprintf(os.Stderr, "mcp error: %v\n", err)
				os.Exit(1)
			}
			return
		case "schema":
			mcp.ExportSchema()
			return
		}
	}

	exitCode := cli.RunCLI(os.Args[1:])
	os.Exit(exitCode)
}
