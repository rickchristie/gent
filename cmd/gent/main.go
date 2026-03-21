// Command gent provides development tools for the gent framework.
//
// Usage:
//
//	gent <command> [arguments]
//
// Commands:
//
//	setup onnx    Install native libraries (libtokenizers, libonnxruntime)
//	model list    List available embedding models
//	model download <name>  Download an embedding model
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "setup":
		if len(os.Args) < 3 || os.Args[2] != "onnx" {
			fmt.Println("Usage: gent setup onnx")
			os.Exit(1)
		}
		runSetup()
	case "model":
		if len(os.Args) < 3 {
			fmt.Println("Usage: gent model <list|download> [name]")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "list":
			runModelList()
		case "download":
			if len(os.Args) < 4 {
				fmt.Println("Usage: gent model download <name>")
				fmt.Println()
				fmt.Println("Run 'gent model list' to see available models.")
				os.Exit(1)
			}
			runModelDownload(os.Args[3])
		default:
			fmt.Printf("Unknown model command: %s\n", os.Args[2])
			fmt.Println("Usage: gent model <list|download> [name]")
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: gent <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  setup onnx              Install native libraries (libtokenizers, libonnxruntime)")
	fmt.Println("  model list              List available embedding models")
	fmt.Println("  model download <name>   Download an embedding model")
}
