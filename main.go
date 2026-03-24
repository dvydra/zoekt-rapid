package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "discover":
		cmdDiscover(os.Args[2:])
	case "poll":
		cmdPoll(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "reindex":
		cmdReindex(os.Args[2:])
	case "rescan":
		cmdRescan(os.Args[2:])
	case "version":
		fmt.Println("zoekt-rapid", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `zoekt-rapid - fast code search with working tree awareness

Commands:
  discover    Scan for git repos under configured roots
  poll        Poll repos for state changes (debug mode)
  serve       Start the search proxy
  status      Show repo states from running proxy
  reindex     Trigger reindex (all or specific repo)
  rescan      Re-discover repos
  version     Print version
  help        Show this help`)
}

func cmdDiscover(args []string) {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	rootsFlag := fs.String("roots", "", "comma-separated root directories (default: ~/src)")
	depthFlag := fs.Int("depth", 0, "max scan depth (default: 3)")
	fs.Parse(args)

	cfg := DefaultConfig()

	if *rootsFlag != "" {
		cfg.Roots = strings.Split(*rootsFlag, ",")
	}
	if *depthFlag > 0 {
		cfg.ScanDepth = *depthFlag
	}

	repos, err := DiscoverRepos(cfg.Roots, cfg.ScanDepth, cfg.ExcludePatterns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Found %d repos:\n", len(repos))
	for _, repo := range repos {
		fmt.Println(repo)
	}
}

func cmdPoll(args []string) {
	fs := flag.NewFlagSet("poll", flag.ExitOnError)
	rootsFlag := fs.String("roots", "", "comma-separated root directories (default: ~/src)")
	fs.Parse(args)

	cfg := DefaultConfig()
	if *rootsFlag != "" {
		cfg.Roots = strings.Split(*rootsFlag, ",")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	state := NewStateTable()
	poller := NewPoller(cfg, state)

	fmt.Fprintln(os.Stderr, "polling repos (ctrl-c to stop)...")
	poller.Run(ctx)
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	rootsFlag := fs.String("roots", "", "comma-separated root directories (default: ~/src)")
	portFlag := fs.Int("port", 0, "proxy listen port (default: 6071)")
	zoektFlag := fs.String("zoekt", "", "upstream zoekt URL (default: http://localhost:6070)")
	fs.Parse(args)

	cfg := DefaultConfig()
	if *rootsFlag != "" {
		cfg.Roots = strings.Split(*rootsFlag, ",")
	}
	if *portFlag > 0 {
		cfg.ProxyPort = *portFlag
	}
	if *zoektFlag != "" {
		cfg.ZoektURL = *zoektFlag
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	state := NewStateTable()
	proxy := NewSearchProxy(cfg.ZoektURL, state)
	reindexMgr := NewReindexManager(cfg, state, proxy)
	poller := NewPoller(cfg, state)
	poller.reindex = reindexMgr
	poller.proxy = proxy
	scheduler := NewScheduler(cfg, reindexMgr)
	srv := NewServer(proxy, state, reindexMgr, poller, scheduler, cfg.ProxyPort, cfg.ZoektURL)

	// Refresh repo map from zoekt on startup (needed for smart startup).
	proxy.RefreshRepoMap()

	// Start poller in background.
	go poller.Run(ctx)

	// Start hourly reindex scheduler.
	go scheduler.Run(ctx)

	// Periodically refresh the zoekt repo name map.
	go func() {
		ticker := time.NewTicker(cfg.DiscoveryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				proxy.RefreshRepoMap()
			}
		}
	}()

	fmt.Fprintf(os.Stderr, "zoekt-rapid proxy on :%d → %s\n", cfg.ProxyPort, cfg.ZoektURL)

	// Run server (blocks until error or signal).
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			cancel()
		}
	}()

	<-ctx.Done()
	fmt.Fprintln(os.Stderr, "\nshutting down, waiting for in-flight reindex jobs...")
	reindexMgr.Wait()
	fmt.Fprintln(os.Stderr, "done")
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	portFlag := fs.Int("port", 6071, "proxy port to query")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost:%d/api/status", *portFlag)
	resp, err := http.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: cannot reach zoekt-rapid on port %d: %v\n", *portFlag, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var status StatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("zoekt-rapid — %d repos, up %s\n", status.RepoCount, status.Uptime)
	fmt.Printf("next full reindex: %s\n\n", status.NextFullReindex)

	// Sort repos by path.
	paths := make([]string, 0, len(status.Repos))
	for p := range status.Repos {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Find common root prefix for relative display.
	home, _ := os.UserHomeDir()
	srcRoot := filepath.Join(home, "src") + "/"

	for _, path := range paths {
		r := status.Repos[path]
		// Show relative path from ~/src, with parent dirs dimmed.
		relPath := path
		if cut, ok := strings.CutPrefix(path, srcRoot); ok {
			relPath = cut
		}
		dir := filepath.Dir(relPath)
		base := filepath.Base(relPath)
		var name string
		if dir == "." {
			name = base
		} else {
			name = fmt.Sprintf("\033[90m%s/\033[0m%s", dir, base)
		}
		dirty := ""
		if r.DirtyFiles > 0 {
			dirty = fmt.Sprintf(" (%d dirty)", r.DirtyFiles)
		}
		reindexing := ""
		if r.Reindexing {
			reindexing = " [reindexing]"
		}
		stale := ""
		if r.Status == "stale" || r.Status == "error" {
			stale = fmt.Sprintf(" [%s]", r.Status)
		}
		// Pad based on visible length (relPath), not the ANSI-colored name.
		padding := 30 - len(relPath)
		if padding < 1 {
			padding = 1
		}
		fmt.Printf("  %s%*s%s@%s%s%s%s\n", name, padding, "", r.Branch, shortCLISHA(r.HeadSHA), dirty, stale, reindexing)
	}
}

func cmdReindex(args []string) {
	fs := flag.NewFlagSet("reindex", flag.ExitOnError)
	portFlag := fs.Int("port", 6071, "proxy port to query")
	fs.Parse(args)

	remaining := fs.Args()
	var url string
	if len(remaining) > 0 {
		url = fmt.Sprintf("http://localhost:%d/api/reindex/%s", *portFlag, remaining[0])
	} else {
		url = fmt.Sprintf("http://localhost:%d/api/reindex", *portFlag)
	}

	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "error: %s\n", string(body))
		os.Exit(1)
	}
	fmt.Println(string(body))
}

func cmdRescan(args []string) {
	fs := flag.NewFlagSet("rescan", flag.ExitOnError)
	portFlag := fs.Int("port", 6071, "proxy port to query")
	fs.Parse(args)

	url := fmt.Sprintf("http://localhost:%d/api/rescan", *portFlag)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Println(string(body))
}

func shortCLISHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
