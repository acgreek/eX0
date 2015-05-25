// +build chan,!tcp

package main

import "time"

// TCP and UDP via local channels. Requires `go test -tags=chan`.
func testFullConnection() {
	components.server = startServer(true) // Wait for server to start listening.
	client()
	time.Sleep(10 * time.Second) // Wait 10 seconds before exiting.
}
