package ir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store is the on-disk layout of the knowledge plane inside a code repo:
//
//	<root>/.team-context/
//	├── facts/       one fact per file (git merge = set union)
//	├── playbooks/   one topic per file
//	└── queue/       transcripts enqueued by hooks, waiting for distillation
//
// MVP deliberately lives inside the existing code repo instead of a separate
// knowledge repo (docs/design-v2.md §13): permissions, clone and CI ride along.
type Store struct {
	Root string // repo root
}

// DirName is the knowledge-plane directory inside a repo.
const DirName = ".team-context"

// Dir returns the .team-context directory of the store.
func (s Store) Dir() string { return filepath.Join(s.Root, DirName) }

// FactsDir returns the facts directory.
func (s Store) FactsDir() string { return filepath.Join(s.Dir(), "facts") }

// PlaybooksDir returns the playbooks directory.
func (s Store) PlaybooksDir() string { return filepath.Join(s.Dir(), "playbooks") }

// QueueDir returns the pending-transcript queue directory.
func (s Store) QueueDir() string { return filepath.Join(s.Dir(), "queue") }

// LocalDir returns the machine-private state directory (distillation
// ledger, caches). Gitignored: its contents are machine state, not
// team knowledge.
func (s Store) LocalDir() string { return filepath.Join(s.Dir(), ".local") }

// MaterializedPath returns the rendered context file consumed via CLAUDE.md.
func (s Store) MaterializedPath() string { return filepath.Join(s.Dir(), "materialized.md") }

// Init creates the store layout. It is idempotent.
func (s Store) Init() error {
	for _, dir := range []string{s.FactsDir(), s.PlaybooksDir(), s.QueueDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	// Keep the knowledge dirs in git, but never queue/ or .local/: both hold
	// machine-private state (transcript paths, distillation ledger).
	// Upgrade-in-place so repos initialized by older versions gain new rules.
	gitignore := filepath.Join(s.Dir(), ".gitignore")
	existing, err := os.ReadFile(gitignore)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(existing)
	changed := false
	for _, rule := range []string{"queue/", ".local/"} {
		if !containsLine(content, rule) {
			if content != "" && !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			content += rule + "\n"
			changed = true
		}
	}
	if changed {
		if err := os.WriteFile(gitignore, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func containsLine(content, line string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// Exists reports whether the store has been initialized.
func (s Store) Exists() bool {
	info, err := os.Stat(s.Dir())
	return err == nil && info.IsDir()
}

// LoadFacts reads every fact file under facts/. Malformed files are returned
// as errors keyed by filename rather than aborting the whole load, so one bad
// merge cannot brick materialization.
func (s Store) LoadFacts() ([]Fact, map[string]error, error) {
	entries, err := os.ReadDir(s.FactsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var facts []Fact
	bad := map[string]error{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.FactsDir(), e.Name()))
		if err != nil {
			bad[e.Name()] = err
			continue
		}
		f, err := ParseFact(data)
		if err != nil {
			bad[e.Name()] = err
			continue
		}
		if f.Filename() != e.Name() {
			bad[e.Name()] = fmt.Errorf("ir: id %q does not match filename", f.ID)
			continue
		}
		facts = append(facts, f)
	}
	SortFacts(facts)
	if len(bad) == 0 {
		bad = nil
	}
	return facts, bad, nil
}

// LoadPlaybooks reads every playbook under playbooks/, same error contract
// as LoadFacts.
func (s Store) LoadPlaybooks() ([]Playbook, map[string]error, error) {
	entries, err := os.ReadDir(s.PlaybooksDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var pbs []Playbook
	bad := map[string]error{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.PlaybooksDir(), e.Name()))
		if err != nil {
			bad[e.Name()] = err
			continue
		}
		p, err := ParsePlaybook(data)
		if err != nil {
			bad[e.Name()] = err
			continue
		}
		pbs = append(pbs, p)
	}
	if len(bad) == 0 {
		bad = nil
	}
	return pbs, bad, nil
}

// WriteFact writes a fact to its canonical location. If a fact with the same
// ID exists and the new observation is newer, the old fact is re-written as
// superseded history under the new fact's Supersedes pointer. Overwriting an
// identical encoding is a no-op (git sees no change).
func (s Store) WriteFact(f Fact) error {
	if err := f.Validate(); err != nil {
		return err
	}
	// Encoding is second-precision (RFC3339), so an in-memory timestamp with
	// a sub-second component would always compare as newer than its own
	// re-parsed self — forging a supersedes entry on every rewrite. Normalize
	// to the stored precision before comparing.
	f.ObservedAt = f.ObservedAt.Truncate(time.Second)
	path := filepath.Join(s.FactsDir(), f.Filename())
	if prev, err := os.ReadFile(path); err == nil {
		old, perr := ParseFact(prev)
		if perr == nil {
			if !f.ObservedAt.After(old.ObservedAt) {
				// Older or same-age observation of an existing key: keep current.
				return nil
			}
			if f.Supersedes == "" {
				f.Supersedes = fmt.Sprintf("%s@%s", old.ID, old.ObservedAt.Format("2006-01-02"))
			}
		}
	}
	return os.WriteFile(path, f.Encode(), 0o644)
}

// WritePlaybook writes a playbook to its canonical location.
func (s Store) WritePlaybook(p Playbook) error {
	if err := p.Validate(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.PlaybooksDir(), p.Filename()), p.Encode(), 0o644)
}
