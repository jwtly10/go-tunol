package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
)

// This is an example server with a flag [-port] for setting the port to run the server on
// Used to test the integration of the server with the tunnel package locally
func main() {
	port := flag.String("port", "3000", "Port to run the server on")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello from local server! Full raw Path: %s\n", r.URL.Path)
	})

	http.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Failed request", http.StatusInternalServerError)
	})

	http.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "API Test endpoint reached! Method: %s\n", r.Method)
	})

	fmt.Printf("Starting example server on :%s\n", *port)
	if err := http.ListenAndServe(fmt.Sprintf(":%s", *port), nil); err != nil {
		log.Fatal(err)
	}
}
