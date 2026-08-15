package main

import (
	"net/http"
	"os"
	"os/exec"
)

// Config is read from the environment. One of these is a secret and must be
// flagged as such; the other is ordinary configuration.
func config() (string, string) {
	return os.Getenv("PAYMENTS_API_KEY"), os.Getenv("SERVICE_TIER")
}

func serve() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {})
	mux.HandleFunc("/orders", func(w http.ResponseWriter, r *http.Request) {})
	http.ListenAndServe(":8080", mux)
}

// revision shells out to git — a process boundary.
func revision() ([]byte, error) {
	return exec.Command("git", "rev-parse", "HEAD").Output()
}
