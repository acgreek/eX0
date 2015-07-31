// eX0-go is a work in progress Go implementation of eX0.
//
// The client runs as a native desktop app and in browser.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

const debugValidation = true

var startedProcess = time.Now()

// THINK: Is this the best way?
var components struct {
	logic  *logic
	server *server
	client *client
}

func main() {
	flag.Parse()

	switch args := flag.Args(); {
	case len(args) == 1 && args[0] == "client":
		components.client = startClient()
		time.Sleep(10 * time.Second) // Wait 10 seconds before exiting.
	case len(args) == 1 && args[0] == "server":
		components.logic = startLogic()
		components.server = startServer()
		select {}
	case len(args) == 1 && args[0] == "server-view":
		components.logic = startLogic()
		components.server = startServer()
		view(false)
	case len(args) == 1 && args[0] == "client-view":
		components.logic = startLogic()
		components.client = startClient()
		view(true)
		components.logic.quit <- struct{}{}
		<-components.logic.quit
	case len(args) == 1 && (args[0] == "client-server-view" || args[0] == "server-client-view"):
		components.logic = startLogic()
		components.server = startServer()
		components.client = startClient()
		view(true)
		components.logic.quit <- struct{}{}
		<-components.logic.quit
	default:
		fmt.Fprintf(os.Stderr, "invalid usage: %q\n", args)
		os.Exit(2)
	}
}
