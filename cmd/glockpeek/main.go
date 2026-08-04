// Command glockpeek serves the glocker stats dashboard (the web interface) as a
// standalone process, separate from the glocker daemon. It reads the same
// /etc/glocker/config.yaml for its listen address and database, serves the
// dashboard on localhost only, and exposes an ingest API the glocker syncer
// pushes local records into.
//
// This replaces the old command-line log viewer — the web dashboard has taken
// its place. Run it as its own service (see extras/glockpeek.service); the
// glocker daemon no longer serves the dashboard itself.
package main

import (
	"flag"
	"log"
	"net/http"

	"glocker/internal/config"
	"glocker/internal/stats"
	"glocker/internal/store"
)

const defaultListen = "127.0.0.1:4317"

func main() {
	listen := flag.String("listen", "", "address to serve on (overrides config; default "+defaultListen+")")
	dbDSN := flag.String("db", "", "database DSN (overrides config; e.g. a sqlite file path for dev)")
	flag.Parse()

	addr := defaultListen
	driver := config.DefaultDatabaseDriver
	dsn := config.DefaultDatabaseDSN

	// Read the same config the daemon uses for the listen address + database.
	// Non-fatal if absent (flags/defaults still resolve).
	if cfg, err := config.LoadConfig(); err != nil {
		log.Printf("glockpeek: could not load %s (%v); using defaults", config.GlockerConfigFile, err)
	} else {
		if cfg.GlockpeekListen != "" {
			addr = cfg.GlockpeekListen
		}
		if cfg.Database.Driver != "" {
			driver = cfg.Database.Driver
		}
		if cfg.Database.DSN != "" {
			dsn = cfg.Database.DSN
		}
	}
	if *listen != "" {
		addr = *listen
	}
	if *dbDSN != "" {
		dsn = *dbDSN
	}

	db, err := store.Open(store.Options{Driver: driver, DSN: dsn})
	if err != nil {
		log.Fatalf("glockpeek: open database (%s): %v", driver, err)
	}

	mux := http.NewServeMux()
	stats.Register(mux, db)

	log.Printf("glockpeek serving the dashboard at http://%s/ (localhost only); db %s: %s", addr, driver, dsn)
	log.Fatal(http.ListenAndServe(addr, mux))
}
