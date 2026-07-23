// Command stillroomd serves a read-only, org-wide view of many repos'
// knowledge planes: cross-repo search over the facts and playbooks that
// `still distill` produced.
//
// It is deliberately a single binary with no database. All state is derived
// from `.team-context/` directories in git checkouts on disk; delete the
// container and nothing is lost, because the server was never the source of
// truth (docs/design-v2.md §17).
//
// Two invariants the server must never break:
//
//   - It never reads the evidence plane. Transcripts stay on the machine that
//     produced them. Only distilled, already-reviewed, already-committed
//     knowledge is served here.
//   - It never writes to a repo. Knowledge changes ride pull requests, in the
//     tool the team already reviews with.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/A4ever369/Stillroom/internal/index"
	"github.com/A4ever369/Stillroom/internal/ir"
)

var version = "dev"

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(s string) error { *m = append(*m, s); return nil }

func main() {
	log.SetFlags(0)
	log.SetPrefix("stillroomd: ")

	var repoFlags, scanFlags multiFlag
	addr := flag.String("addr", ":8080", "listen address")
	refresh := flag.Duration("refresh", time.Minute, "how often to rebuild the index from disk")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Var(&repoFlags, "repo", "a repo to index as name=path (repeatable; name defaults to the directory name)")
	flag.Var(&scanFlags, "scan", "directory to scan for repos containing .team-context/ (repeatable)")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Println("stillroomd", version)
		return
	}

	repos, err := resolveRepos(repoFlags, scanFlags)
	if err != nil {
		log.Fatal(err)
	}
	if len(repos) == 0 {
		flag.Usage()
		log.Fatal("no repos to index — pass -repo or -scan")
	}

	srv := &server{}
	srv.rebuild(repos)
	go srv.refreshLoop(repos, *refresh)

	log.Printf("indexing %d repo(s), %d document(s)", len(repos), srv.index().Len())
	for _, r := range srv.index().Repos() {
		log.Printf("  %-40s %d facts, %d playbooks", r.Name, r.Facts, r.Playbooks)
	}
	log.Printf("listening on http://localhost%s", *addr)

	if err := http.ListenAndServe(*addr, srv.routes()); err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `stillroomd — org-wide search over your team's distilled knowledge.

Usage:
  stillroomd -scan ~/code
  stillroomd -repo acme/infra=/srv/checkouts/infra -repo acme/web=/srv/checkouts/web

Flags:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
The server holds no source of truth: it indexes .team-context/ directories
from git checkouts and can be rebuilt from scratch at any time. It never
reads session transcripts and never writes to a repo.
`)
}

// server holds the current index behind an atomic pointer: rebuilds swap a
// fully-built snapshot in, so a request never observes a half-built index and
// no lock is held during a rebuild.
type server struct {
	ix atomic.Pointer[index.Index]
}

func (s *server) index() *index.Index { return s.ix.Load() }

func (s *server) rebuild(repos []index.Repo) {
	ix := index.Build(repos, time.Now())
	for name, err := range ix.Bad {
		log.Printf("skipped %s: %v", name, err)
	}
	s.ix.Store(ix)
}

func (s *server) refreshLoop(repos []index.Repo, every time.Duration) {
	if every <= 0 {
		return
	}
	for range time.Tick(every) {
		s.rebuild(repos)
	}
}

// resolveRepos turns the -repo/-scan flags into a deduplicated, deterministic
// repo list. A path that does not hold a knowledge base is an error when named
// explicitly, and simply not a match when scanning.
func resolveRepos(explicit, scans multiFlag) ([]index.Repo, error) {
	seen := map[string]bool{}
	var out []index.Repo

	add := func(name, path string) {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		if seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, index.Repo{Name: name, Path: abs})
	}

	for _, spec := range explicit {
		name, path, ok := strings.Cut(spec, "=")
		if !ok {
			path, name = name, filepath.Base(strings.TrimRight(name, string(filepath.Separator)))
		}
		if !hasStore(path) {
			return nil, fmt.Errorf("%s: no %s/ directory — run `still init` there first", path, ir.DirName)
		}
		add(name, path)
	}

	for _, root := range scans {
		found, err := scanRepos(root)
		if err != nil {
			return nil, err
		}
		for _, r := range found {
			add(r.Name, r.Path)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func hasStore(root string) bool {
	return ir.Store{Root: root}.Exists()
}

// scanRepos walks root looking for directories that contain a knowledge base.
// It does not descend into a repo once found, and skips the usual heavy
// directories so scanning a whole ~/code tree stays fast.
func scanRepos(root string) ([]index.Repo, error) {
	var out []index.Repo
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree is skipped, never fatal: one bad
			// permission must not stop the whole scan.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case "node_modules", "vendor", "target", "dist", "build", ".venv":
			return fs.SkipDir
		}
		if strings.HasPrefix(d.Name(), ".") && path != root {
			return fs.SkipDir
		}
		if !hasStore(path) {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil || rel == "." {
			rel = filepath.Base(path)
		}
		out = append(out, index.Repo{Name: filepath.ToSlash(rel), Path: path})
		return fs.SkipDir
	})
	if err != nil && !errors.Is(err, fs.SkipAll) {
		return nil, fmt.Errorf("scan %s: %w", root, err)
	}
	return out, nil
}
