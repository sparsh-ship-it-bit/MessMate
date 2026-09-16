package main

import (
    "net/http"
    "os"
)

// registerWeb serves the compiled React application from the same origin as the API.
// This keeps the first online deployment simple: one web service + one Postgres database.
func registerWeb(mux *http.ServeMux) {
    dir := os.Getenv("WEB_DIR")
    if dir == "" {
        dir = "web/dist"
    }
    mux.Handle("/", http.FileServer(http.Dir(dir)))
}
