package main

import (
	"fmt"
	"log"
	"os"
)

func usage() {
	fmt.Fprintf(os.Stderr, "Ralph Orchestrator (WIP)\n")
	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  ralph issue [flags] <issue-number>\n")
	fmt.Fprintf(os.Stderr, "  ralph review [flags] <pr-number>\n")
	fmt.Fprintf(os.Stderr, "  ralph sync-branch [flags] <pr-number>\n")
	fmt.Fprintf(os.Stderr, "  ralph prd new|from-issue <issue-number>\n")
	fmt.Fprintf(os.Stderr, "  ralph chat\n")
}

func main() {
	log.SetFlags(log.LstdFlags)

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	subcmd := os.Args[1]
	switch subcmd {
	case "issue":
		log.Println("TODO: implement ralph issue")
	case "review":
		log.Println("TODO: implement ralph review")
	case "sync-branch":
		log.Println("TODO: implement ralph sync-branch")
	case "prd":
		log.Println("TODO: implement ralph prd")
	case "chat":
		log.Println("TODO: implement ralph chat")
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(1)
	}
}
