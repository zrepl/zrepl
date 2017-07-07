package main

import (
	"log"
	"os"
	"os/exec"
	"time"
)

var config struct {
	duration time.Duration
}

func usage() {
	log.Printf("usage: repeat repeatInterval command [args...]")
}

func main() {

	args := os.Args

	if len(args) < 3 {
		usage()
		os.Exit(1)
	}
	repeatInterval, err := time.ParseDuration(args[1])
	if err != nil {
		log.Printf("cannot parse interval: %s", err)
		usage()
		os.Exit(1)
	}

	var lastStart time.Time

	for {
		cmd := exec.Command(args[2], args[3:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		lastStart = time.Now()
		if err := cmd.Run(); err != nil {
			panic(err)
		}
		time.Sleep(lastStart.Add(repeatInterval).Sub(time.Now()))
	}

}
