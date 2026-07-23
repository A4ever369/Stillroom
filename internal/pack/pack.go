// Package pack is the transport format for handing your accumulated
// experience to another person.
//
// The knowledge plane already merges through git. A pack is the same knowledge
// travelling a different road: one file, one link, no repository in common.
// You send it to someone; they open it in their own agent and pick up where
// you left off — instead of reading a document you wrote about it.
//
// A pack carries two layers, and the distinction is load-bearing:
//
//   - The knowledge layer — facts and playbooks. What stays true. Always
//     present.
//   - The evidence layer — redacted session digests. How you got there. The
//     distilled facts say pgbouncer is on 6432; the session shows the two
//     wrong turns taken before finding that out, which is often the part that
//     actually transfers.
//
// The evidence layer is a deliberate, documented departure from the project's
// original privacy invariant ("evidence never leaves the machine that produced
// it"). The rule is now: evidence leaves only by an explicit per-publish
// decision, showing exactly what and how much. Sharing a pack is an act
// between people who already trust each other; the tool's job is to make sure
// nobody does it by accident.
//
// A pack ships as a single JSON document rather than an archive, because the
// person receiving it has to be able to read it before trusting it. Anything a
// pack contains ends up inside the receiver's agent context — see Merge for
// why that is treated as data and never as instructions.
package pack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/A4ever369/Stillroom/internal/ir"
	"github.com/A4ever369/Stillroom/internal/redact"
	"github.com/A4ever369/Stillroom/internal/session"
)

// Version is the pack format version. A reader must refuse a pack it does not
// understand rather than guess: packs cross machines and versions freely.
const Version = 1

// MaxSessionBytes bounds one embedded session digest. Same budget as the
// distiller's input — it is the same rendering, and it keeps a pack something
// a person can actually read before trusting it.
const MaxSessionBytes = session.MaxDigestBytes

// Mode is what the publisher chose to send. It is recorded in the pack rather
// than inferred from whether Sessions happens to be empty, because the
// receiver is entitled to know which decision was made about them: a pack with
// no sessions could mean "I protected my evidence" or "there was none", and
// those are different statements.
type Mode string

const (
	// ModeKnowledge sends what stays true and nothing else. Evidence never
	// leaves the publisher's machine — the project's original privacy floor.
	ModeKnowledge Mode = "knowledge"
	// ModeFull additionally sends redacted session digests: the reasoning, the
	// wrong turns, the part that transfers know-how rather than conclusions.
	// A deliberate, per-publish decision between people who already trust each
	// other.
	ModeFull Mode = "full"
)

// Valid reports whether m is a mode this build understands.
func (m Mode) Valid() bool { return m == ModeKnowledge || m == ModeFull }

// Pack is the wire format. Field order here is the encoding order; keep it
// stable so the same content always hashes to the same id.
type Pack struct {
	Version int  `json:"version"`
	Mode    Mode `json:"mode"`
	// Note is the one line the publisher writes: "how our deploy actually works".
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// Publisher is set by the hub from the authenticated account. It is the
	// receiver's only handle on "should I trust this", so it is never
	// self-asserted by the CLI.
	Publisher string `json:"publisher,omitempty"`
	// Origin describes where the knowledge came from, without leaking anything
	// about the machine it came from — repo name, never an absolute path.
	Origin    Origin         `json:"origin"`
	Facts     []ir.Fact      `json:"facts"`
	Playbooks []ir.Playbook  `json:"playbooks"`
	Sessions  []SessionEntry `json:"sessions,omitempty"`
}

// Origin is deliberately thin. "Which repo" helps the receiver judge whether
// the knowledge applies to them; "which directory on whose laptop" helps
// nobody and is machine-private state.
type Origin struct {
	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch,omitempty"`
	Tool   string `json:"tool,omitempty"`
}

// SessionEntry is the evidence layer: one redacted, size-bounded session
// digest. Never the raw transcript — that is tens of megabytes, contains
// everything the tool ever echoed, and no agent can read it anyway.
type SessionEntry struct {
	Ref   string    `json:"ref"`
	Tool  string    `json:"tool,omitempty"`
	Turns int       `json:"turns,omitempty"`
	At    time.Time `json:"at"`
	Text  string    `json:"text"`
	// Redactions counts secret-shaped strings scrubbed from Text. Shown to the
	// publisher before upload: a high number means the session was handling
	// credentials and deserves a closer look, not blind trust in the scrubber.
	Redactions int `json:"redactions,omitempty"`
}

// Build assembles a pack from a knowledge base plus, in ModeFull, the session
// digests behind it. Only active facts travel: superseded and disputed
// knowledge is lineage, and shipping it to someone with no history to attach
// it to would just be noise.
//
// In ModeKnowledge the sessions argument is ignored entirely — not filtered
// later, not carried and dropped at encode time. The privacy choice is
// enforced at the point the pack is built, so there is no path by which
// evidence reaches the wire in knowledge mode.
//
// Every embedded session is redacted again here regardless of what upstream
// already did. Redaction is cheap, and this is the last point before content
// leaves the machine.
func Build(s ir.Store, sessions []session.Digest, mode Mode, note string, origin Origin) (Pack, error) {
	if !mode.Valid() {
		return Pack{}, fmt.Errorf("pack: unknown mode %q", mode)
	}
	facts, _, err := s.LoadFacts()
	if err != nil {
		return Pack{}, err
	}
	playbooks, _, err := s.LoadPlaybooks()
	if err != nil {
		return Pack{}, err
	}

	p := Pack{
		Version:   Version,
		Mode:      mode,
		Note:      strings.TrimSpace(note),
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		Origin:    origin,
	}
	for _, f := range facts {
		if f.Status == ir.StatusActive {
			p.Facts = append(p.Facts, f)
		}
	}
	sort.Slice(p.Facts, func(i, j int) bool { return p.Facts[i].ID < p.Facts[j].ID })

	p.Playbooks = append(p.Playbooks, playbooks...)
	sort.Slice(p.Playbooks, func(i, j int) bool { return p.Playbooks[i].ID < p.Playbooks[j].ID })

	if mode != ModeFull {
		sessions = nil
	}
	for _, d := range sessions {
		text, n := redact.Text(session.Clip(d.Text, MaxSessionBytes))
		if strings.TrimSpace(text) == "" {
			continue
		}
		p.Sessions = append(p.Sessions, SessionEntry{
			Ref:        d.Meta.Tool + "://" + d.Meta.SessionID,
			Tool:       d.Meta.Tool,
			Turns:      d.Meta.Turns,
			At:         d.Meta.LastActivity.UTC(),
			Text:       text,
			Redactions: n,
		})
	}
	sort.Slice(p.Sessions, func(i, j int) bool { return p.Sessions[i].Ref < p.Sessions[j].Ref })

	return p, nil
}

// Encode renders the pack as indented JSON. Indented on purpose: a pack is
// meant to be read by the person deciding whether to trust it, and a wall of
// minified JSON is not readable. Encoding is deterministic, so the same
// content always produces the same bytes and the same ID.
func (p Pack) Encode() ([]byte, error) {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ID is the content hash, short enough to appear in a link and long enough not
// to collide. Two identical packs get the same id, so re-publishing unchanged
// knowledge is idempotent.
func (p Pack) ID() string {
	b, err := p.Encode()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// Decode parses a pack and rejects anything it cannot vouch for. A pack
// arrives from another machine, so every field is untrusted input: a version
// from the future is refused rather than half-read, and malformed knowledge is
// dropped rather than written into the receiver's store.
func Decode(data []byte) (Pack, []error, error) {
	var p Pack
	var bad []error
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&p); err != nil {
		return Pack{}, nil, fmt.Errorf("pack: not a readable pack: %w", err)
	}
	if p.Version == 0 {
		return Pack{}, nil, fmt.Errorf("pack: missing version")
	}
	if p.Version > Version {
		return Pack{}, nil, fmt.Errorf("pack: version %d is newer than this build understands (%d) — upgrade `still`", p.Version, Version)
	}
	if !p.Mode.Valid() {
		return Pack{}, nil, fmt.Errorf("pack: unknown mode %q", p.Mode)
	}
	// A knowledge-mode pack carrying sessions is either corrupt or forged.
	// Believe the declared mode and drop the payload rather than quietly
	// handing the receiver evidence the publisher said they were not sending.
	if p.Mode == ModeKnowledge && len(p.Sessions) > 0 {
		bad = append(bad, fmt.Errorf("pack: %d session(s) present in a knowledge-only pack — dropped", len(p.Sessions)))
		p.Sessions = nil
	}

	facts := p.Facts[:0]
	for _, f := range p.Facts {
		if err := f.Validate(); err != nil {
			bad = append(bad, err)
			continue
		}
		facts = append(facts, f)
	}
	p.Facts = facts

	books := p.Playbooks[:0]
	for _, b := range p.Playbooks {
		if err := b.Validate(); err != nil {
			bad = append(bad, err)
			continue
		}
		books = append(books, b)
	}
	p.Playbooks = books

	for i := range p.Sessions {
		p.Sessions[i].Text = session.Clip(p.Sessions[i].Text, MaxSessionBytes)
	}
	return p, bad, nil
}

// Size reports the encoded byte size, for the publisher's confirmation prompt.
func (p Pack) Size() int {
	b, _ := p.Encode()
	return len(b)
}

// Redactions totals the secret-shaped strings scrubbed from embedded sessions.
func (p Pack) Redactions() int {
	n := 0
	for _, s := range p.Sessions {
		n += s.Redactions
	}
	return n
}
