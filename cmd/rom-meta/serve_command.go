package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rommeta/internal/webserver"
)

func runServe(args []string) error {
	const defaultAddr = "127.0.0.1:8787"
	options := webserver.Options{DataRoot: ".", Addr: defaultAddr, Token: os.Getenv("ROMMETA_API_TOKEN")}
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(os.Stdout)
	flags.Usage = func() { printServeUsage() }
	flags.StringVar(&options.DataRoot, "data-root", ".", "Catalog root")
	flags.StringVar(&options.Addr, "addr", defaultAddr, "One HTTP listen address")
	flags.StringVar(&options.ProfilePath, "profile", "", "Optional selection profile; defaults follow sources.yaml")
	flags.StringVar(&options.LookupCacheDir, "lookup-cache-dir", "", "Optional derived index directory; - disables")
	flags.StringVar(&options.ROMRoot, "rom-root", "", "Fixed-selection ROM root")
	flags.StringVar(&options.Platform, "platform", "", "Fixed-selection reader/platform")
	flags.StringVar(&options.Match, "match", "", "Fixed-selection path filter")
	flags.IntVar(&options.Limit, "limit", 0, "Fixed-selection maximum inputs (0: all)")
	flags.BoolVar(&options.SkipUnmatched, "skip-unmatched", false, "Explicitly skip non-exact fixed-selection inputs")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("serve takes flags, not positional arguments; see rom-meta serve --help")
	}
	root, err := filepath.Abs(options.DataRoot)
	if err != nil {
		return err
	}
	options.DataRoot = root
	options.Progress = func(stage, name string, current, total int) {
		fmt.Printf("API %s %d/%d | %s\n", stage, current, total, name)
	}
	// Construct before announcing a working address; initialization errors stay clear.
	handler, err := webserver.New(options)
	if err != nil {
		return err
	}
	fmt.Println("RomMeta catalog server")
	fmt.Printf("Catalog: %s\nURL: %s\nAPI: %sapi\nSwagger: %sdocs\n", root, serveURL(options.Addr), serveURL(options.Addr), serveURL(options.Addr))
	if options.Token == "" {
		fmt.Println("Access: no authentication (default: loopback)")
	} else {
		fmt.Println("Access: optional shared-token protection enabled; browser username: rommeta")
	}
	return webserver.Listen(options.Addr, handler)
}
func serveURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		addr = "127.0.0.1:8787"
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr + "/"
	}
	return "http://" + addr + "/"
}
func printServeUsage() {
	fmt.Println(`rom-meta serve starts one read-only catalog browser, API and Swagger UI.

Usage:
  rom-meta serve --data-root <catalog> [--addr <host:port>] [--profile <yaml>]

Defaults: http://127.0.0.1:8787, built-in selection profile, no required token.
Browser: /. API: /api/* and /v1/*. OpenAPI: /openapi.json. Swagger: /docs.
Set ROMMETA_API_TOKEN (32+ characters) only to enable optional shared protection.
Clients use Bearer; browsers use HTTP Basic (user rommeta, password the same token).
There are no user accounts, roles or sessions. Use TLS outside a trusted network.

Catalog writes remain CLI operations: dat import, fetch and catalog fold.
Export writes frontend output while preserving the catalog and source ROMs.
Stats reports progress and cancels on navigation. Video opens only on explicit play.

Additional options:
  --lookup-cache-dir <dir>  Optional derived index directory; - disables it.
  --rom-root <dir> --platform <reader>  Fixed-selection mode.
  --match <text> --limit <n> --skip-unmatched  Selection filters.`)
}
