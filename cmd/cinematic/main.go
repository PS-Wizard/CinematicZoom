package main

import (
	"flag"
	"fmt"
	"os"

	"cinematic/internal/app"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7788", "listen address")
	noOpen := flag.Bool("no-open", false, "do not open the browser")
	flag.Parse()
	if err := app.Run(*addr, !*noOpen); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
