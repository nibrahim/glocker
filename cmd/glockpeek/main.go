// Command glockpeek serves the glocker stats dashboard (the web interface) as a
// standalone process, separate from the glocker daemon. It reads the same
// /etc/glocker/config.yaml to locate the logs and its listen address, reads the
// log files directly, and serves the dashboard on localhost only.
//
// This replaces the old command-line log viewer — the web dashboard has taken
// its place. Run it as its own service (see extras/glockpeek.service); the
// glocker daemon no longer serves /stats itself.
package main

import (
	"flag"
	"log"
	"net/http"

	"glocker/internal/config"
	"glocker/internal/stats"
)

const defaultListen = "127.0.0.1:4317"

func main() {
	listen := flag.String("listen", "", "address to serve on (overrides config; default "+defaultListen+")")
	flag.Parse()

	addr := defaultListen
	opts := stats.Options{}

	// Read the same config the daemon uses, to locate the usage log / rules file
	// and the configured listen address. Non-fatal if absent (env vars + defaults
	// in the stats package still resolve the log paths).
	if cfg, err := config.LoadConfig(); err != nil {
		log.Printf("glockpeek: could not load %s (%v); using defaults", config.GlockerConfigFile, err)
	} else {
		opts.UsageLog = cfg.UsageMonitor.LogFile
		opts.RulesFile = cfg.UsageMonitor.RulesFile
		if cfg.GlockpeekListen != "" {
			addr = cfg.GlockpeekListen
		}
	}
	if *listen != "" {
		addr = *listen
	}

	mux := http.NewServeMux()
	stats.Register(mux, opts)

	log.Printf("glockpeek serving the dashboard at http://%s/stats/ (localhost only)", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
