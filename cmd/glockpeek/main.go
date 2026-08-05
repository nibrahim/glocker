// Command glockpeek serves the glocker stats dashboard (the web interface) as a
// standalone process, separate from the glocker daemon. It reads the same
// /etc/glocker/config.yaml for its listen address and database, serves the
// dashboard on a per-account login, and exposes a token-authenticated ingest API
// the glocker syncer pushes local records into. Designed to be hosted remotely.
//
// Admin subcommands (run locally, they touch the DB directly):
//
//	glockpeek -adduser <name>        # create a dashboard account (prompts for a password)
//	glockpeek -passwd  <name>        # change a password
//	glockpeek -addtoken <name>       # mint an ingest API token for <name> (printed once)
//
// This replaces the old command-line log viewer; run the server as its own
// service (see extras/glockpeek.service).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"golang.org/x/term"

	"glocker/internal/config"
	"glocker/internal/stats"
	"glocker/internal/store"
)

const defaultListen = "127.0.0.1:4317"

func main() {
	listen := flag.String("listen", "", "address to serve on (overrides config; default "+defaultListen+")")
	dbDSN := flag.String("db", "", "database DSN (overrides config; e.g. a sqlite file path for dev)")
	addUser := flag.String("adduser", "", "create a dashboard account with this username, then exit")
	passwd := flag.String("passwd", "", "change the password for this username, then exit")
	addToken := flag.String("addtoken", "", "mint an ingest API token for this username, then exit")
	flag.Parse()

	addr := defaultListen
	driver := config.DefaultDatabaseDriver
	dsn := config.DefaultDatabaseDSN
	secureCookies := false
	mode := config.GlockpeekModeLocal

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
		secureCookies = cfg.GlockpeekSecureCookies
		if cfg.GlockpeekMode != "" {
			mode = cfg.GlockpeekMode
		}
	}
	if *listen != "" {
		addr = *listen
	}
	if *dbDSN != "" {
		dsn = *dbDSN
	}

	if mode != config.GlockpeekModeLocal && mode != config.GlockpeekModeHosted {
		log.Printf("glockpeek: unknown glockpeek_mode %q; treating as %q", mode, config.GlockpeekModeLocal)
		mode = config.GlockpeekModeLocal
	}
	hosted := mode == config.GlockpeekModeHosted
	// Local mode is a personal-desktop guarantee: bind loopback only, so the
	// dashboard (and its open, tokenless ingest endpoint) is unreachable from
	// other hosts regardless of the configured listen address.
	if !hosted {
		addr = forceLoopback(addr)
	}

	db, err := store.Open(store.Options{Driver: driver, DSN: dsn})
	if err != nil {
		log.Fatalf("glockpeek: open database (%s): %v", driver, err)
	}

	// Admin subcommands: touch the DB and exit, no server.
	switch {
	case *addUser != "":
		runAddUser(db, *addUser)
		return
	case *passwd != "":
		runPasswd(db, *passwd)
		return
	case *addToken != "":
		runAddToken(db, *addToken)
		return
	}

	opts := stats.Options{Auth: hosted, SecureCookies: secureCookies}
	desc := "hosted: login required"
	if !hosted {
		// Local mode: everything runs as one implicit account, ingest is open.
		du, err := db.EnsureDefaultUser()
		if err != nil {
			log.Fatalf("glockpeek: ensure default user: %v", err)
		}
		opts.DefaultUserID = du.ID
		desc = "local: no login, loopback only"
	}

	mux := http.NewServeMux()
	stats.Register(mux, db, opts)

	log.Printf("glockpeek serving the dashboard at http://%s/ (%s); db %s: %s", addr, desc, driver, dsn)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// forceLoopback rewrites addr's host to 127.0.0.1, preserving the port, so local
// mode can never accidentally bind a public interface.
func forceLoopback(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		port = "4317"
	}
	return net.JoinHostPort("127.0.0.1", port)
}

func runAddUser(db *store.DB, username string) {
	pw := readNewPassword()
	if _, err := db.CreateUser(username, pw); err != nil {
		log.Fatalf("glockpeek: create user %q: %v", username, err)
	}
	fmt.Printf("created dashboard account %q\n", username)
}

func runPasswd(db *store.DB, username string) {
	pw := readNewPassword()
	if err := db.SetPassword(username, pw); err != nil {
		log.Fatalf("glockpeek: set password for %q: %v", username, err)
	}
	fmt.Printf("password updated for %q\n", username)
}

func runAddToken(db *store.DB, username string) {
	u, err := db.UserByName(username)
	if err != nil {
		log.Fatalf("glockpeek: no such user %q: %v", username, err)
	}
	tok, err := db.CreateAPIToken(u.ID, "cli")
	if err != nil {
		log.Fatalf("glockpeek: create token: %v", err)
	}
	fmt.Printf("ingest token for %q (store it now — it is not recoverable):\n\n  %s\n\n", username, tok)
	fmt.Println("The glocker syncer sends it as:  Authorization: Bearer <token>")
}

// readNewPassword reads a password without echo from a TTY, or a single line
// from stdin when piped (for scripting). It requires confirmation on a TTY.
func readNewPassword() string {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Print("New password: ")
		p1, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			log.Fatalf("glockpeek: reading password: %v", err)
		}
		fmt.Print("Confirm password: ")
		p2, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			log.Fatalf("glockpeek: reading password: %v", err)
		}
		if string(p1) != string(p2) {
			log.Fatal("glockpeek: passwords do not match")
		}
		if len(p1) == 0 {
			log.Fatal("glockpeek: empty password")
		}
		return string(p1)
	}
	// Piped: read one line.
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		log.Fatal("glockpeek: no password on stdin")
	}
	pw := strings.TrimRight(sc.Text(), "\r\n")
	if pw == "" {
		log.Fatal("glockpeek: empty password")
	}
	return pw
}
