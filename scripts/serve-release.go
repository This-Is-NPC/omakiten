//go:build ignore

// serve-release.go serves a release fixture over loopback and publishes its port.
package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
)

func main() {
	if err := serve(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: serve-release ROOT PORT_FILE")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if err := os.WriteFile(args[1], []byte(strconv.Itoa(port)), 0o600); err != nil {
		return err
	}
	return http.Serve(listener, http.FileServer(http.Dir(args[0])))
}
