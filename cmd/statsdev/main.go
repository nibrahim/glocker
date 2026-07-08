// Command statsdev serves only the /stats dashboard on a local port, for
// developing/verifying the embedded dashboard without the full daemon (which
// needs :80 and root). Throwaway dev helper.
package main

import (
	"log"
	"net/http"
	"os"

	"glocker/internal/stats"
)

func main() {
	addr := "127.0.0.1:4400"
	if len(os.Args) > 1 {
		addr = os.Args[1]
	}
	mux := http.NewServeMux()
	stats.Register(mux, stats.Options{})
	log.Printf("statsdev on http://%s/stats/", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
