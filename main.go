package main

import (
	"cmp"
	"fmt"
	"os"
	"text/tabwriter"

	"portman/internal/ports"
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

	root := procRoot()
	ls, err := ports.List(os.DirFS(root))
	if err != nil {
		fmt.Fprintf(os.Stderr, "portman: reading procfs at %s: %v\n", root, err)
		os.Exit(1)
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "PORT\tPROTO\tADDRESS\tUID\tINODE")
	for _, l := range ls {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%d\n", l.Port, l.Proto, l.Addr, l.UID, l.Inode)
	}
	tw.Flush()
}

// procRoot returns the procfs mount point. HOST_PROC overrides it, following
// the convention used when reading a host's /proc from inside a container.
func procRoot() string {
	return cmp.Or(os.Getenv("HOST_PROC"), "/proc")
}
