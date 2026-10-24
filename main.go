package main

import (
	"fmt"
	"os"
)

const usage = `portman: see what's running on your ports

Usage:
  portman              list listening ports
  portman kill <port>  stop the process using a port
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			fmt.Print(usage)
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
			os.Exit(2)
		}
	}

	fmt.Println("portman: listing not implemented yet")
}
