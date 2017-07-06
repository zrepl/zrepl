// See cmd package.
package main

import (
	"github.com/zrepl/zrepl/cmd"
	"log"
	"os"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		log.Printf("error executing root command: %s", err)
		os.Exit(1)
	}
}
