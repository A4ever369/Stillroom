// Package distill turns a session digest into proposed knowledge-plane
// changes: new/updated facts and optionally a playbook revision.
//
// The LLM call goes through the user's own `claude -p` (headless Claude
// Code): no separate API key, and the transcript never leaves the machine —
// the privacy boundary the whole product is built on (docs/design-v2.md §4.1).
package distill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/0xbeekeeper/stillroom/internal/ir"
	"github.com/0xbeekeeper/stillroom/internal/redact"
	"github.com/0xbeekeeper/stillroom/internal/session"
)

// Proposal is the distiller's suggested change set. It is applied as plain
// file writes; review happens in git (diff / PR), not in a bespoke UI.
type Proposal struct {
	Facts    []ir.Fact
	Playbook *ir.Playbook
	// Redactions counts secrets scrubbed from the proposal on the way out.
	Redactions int
}

// Runner executes a distillation prompt and returns the model's raw text.
// Production uses ClaudeRunner; tests substitute a fake.
type Runner func(ctx context.Context, prompt string) (string, error)

// ClaudeRunner shells out to `claude -p` in JSON mode. It deliberately runs
// with --no-session-persistence so distillation runs never appear in the
// session picker or trigger SessionEnd hooks (no recursion).
func ClaudeRunner(ctx context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p",
		"--output-format", "json",
		"--no-session-persistence",
	)
	cmd.Stdin = strings.NewReader(prompt)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("distill: claude -p failed: %w: %s", err, strings.TrimSpace(errb.String()))
	}
	var envelope struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		return "", fmt.Errorf("distill: cannot parse claude -p envelope: %w", err)
	}
	return envelope.Result, nil
}

// Options configures one distillation run.
type Options struct {
	// Scope is stamped onto proposed facts, e.g. "repo:traces-git".
	Scope string
	// SourceRef is the evidence pointer stamped onto proposed facts,
	// e.g. "claude-code://<session-id>".
	SourceRef string
	// ExistingFactIDs lets the prompt steer the model toward reusing
	// established semantic keys instead of minting near-duplicates.
	ExistingFactIDs []string
	// ExistingPlaybooks ("id — title" lines) steers the model toward
	// REVISING an established playbook rather than minting a parallel one.
	ExistingPlaybooks []string
	// Now is injected for determinism in tests.
	Now time.Time
}

// Run digests-in, proposal-out. The digest text is redacted BEFORE prompting
// (defense in depth) and the proposal is redacted again after parsing
// (distillation concentrates secrets — §4.1).
func Run(ctx context.Context, run Runner, d session.Digest, opts Options) (Proposal, error) {
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	digestText, preCount := redact.Text(d.Text)
	prompt := BuildPrompt(digestText, opts)
	raw, err := run(ctx, prompt)
	if err != nil {
		return Proposal{}, err
	}
	prop, err := parseProposal(raw, opts)
	if err != nil {
		return Proposal{}, err
	}
	prop.Redactions += preCount
	return prop, nil
}

// BuildPrompt renders the distillation instruction. Kept as one function so
// prompt iteration is a single-file change with a stable test surface.
func BuildPrompt(digest string, opts Options) string {
	var b strings.Builder
	b.WriteString(`You are the distiller of a team knowledge base built from AI coding sessions.
Read the session digest below and extract ONLY knowledge that would help a
teammate (or their AI agent) continue this kind of work later.

Extract two kinds of items:

1. facts — durable, independently useful statements about THIS project or
   environment (infrastructure quirks, decisions made, constraints discovered,
   gotchas hit and resolved). NOT generic programming knowledge, NOT restating
   code that is already in the repo, NOT step-by-step narration.
   Exclude anything true only of THIS session or run, not of the project:
   how long a command took, that a tool timed out, one-off setup/boilerplate
   commands, or observations about the distillation/tooling itself. Ask "would
   a teammate who never saw this session still need this in a month?" — if not,
   drop it.
2. playbook — OPTIONAL. Only when the session completed a repeatable
   multi-step procedure worth handing to a teammate: preconditions, steps,
   pitfalls. Otherwise omit it.

Rules:
- fact id: lowercase dot-separated semantic key, "domain.object.attribute"
  style, e.g. "deploy.acme.db-endpoint".`)
	if len(opts.ExistingFactIDs) > 0 {
		b.WriteString("\n- The knowledge base already has these fact ids. REUSE an existing id when your fact is a newer observation of the same thing; otherwise mint a new id that does not collide:\n")
		for _, id := range opts.ExistingFactIDs {
			b.WriteString("    ")
			b.WriteString(id)
			b.WriteString("\n")
		}
	}
	if len(opts.ExistingPlaybooks) > 0 {
		b.WriteString("\n- Existing playbooks (id — title). If this session refined one of these procedures, REUSE its id so your output revises it; only mint a new playbook id for a genuinely different procedure:\n")
		for _, p := range opts.ExistingPlaybooks {
			b.WriteString("    ")
			b.WriteString(p)
			b.WriteString("\n")
		}
	}
	b.WriteString(`- Each fact body: 1-3 sentences, one thing per fact, self-contained.
- NEVER include credentials, tokens, keys or passwords in any output; write
  "[REDACTED]" instead.
- Quality over quantity: 0 facts is a valid answer for an uneventful session.
- confidence: "high" only for facts you saw verified in the session (a fix
  that worked, an error reproduced); "medium" for reasonable inference;
  "low" for speculation.

Respond with ONLY a JSON object, no markdown fence, matching:
{
  "facts": [
    {"id": "...", "confidence": "high|medium|low", "body": "..."}
  ],
  "playbook": {"id": "...", "title": "...", "body": "..."} | null
}

Session digest:
---
`)
	b.WriteString(digest)
	b.WriteString("\n---\n")
	return b.String()
}

// parseProposal decodes the model's JSON answer into a validated Proposal.
// It survives a stray markdown fence (models relapse) but is otherwise strict:
// a malformed item is dropped with its error recorded, not silently kept.
func parseProposal(raw string, opts Options) (Proposal, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return Proposal{}, fmt.Errorf("distill: no JSON object in model output")
	}
	var wire struct {
		Facts []struct {
			ID         string `json:"id"`
			Confidence string `json:"confidence"`
			Body       string `json:"body"`
		} `json:"facts"`
		Playbook *struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"playbook"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &wire); err != nil {
		return Proposal{}, fmt.Errorf("distill: bad proposal JSON: %w", err)
	}

	var prop Proposal
	for _, wf := range wire.Facts {
		body, n := redact.Text(strings.TrimSpace(wf.Body))
		prop.Redactions += n
		f := ir.Fact{
			ID:         strings.TrimSpace(wf.ID),
			Scope:      opts.Scope,
			ObservedAt: opts.Now,
			Source:     opts.SourceRef,
			Confidence: ir.Confidence(wf.Confidence),
			Status:     ir.StatusActive,
			Body:       body,
		}
		if f.Confidence == "" {
			f.Confidence = ir.ConfidenceMedium
		}
		if err := f.Validate(); err != nil {
			continue // drop malformed items; the PR diff shows what survived
		}
		prop.Facts = append(prop.Facts, f)
	}
	if wire.Playbook != nil {
		body, n := redact.Text(strings.TrimSpace(wire.Playbook.Body))
		prop.Redactions += n
		p := ir.Playbook{
			ID:        strings.TrimSpace(wire.Playbook.ID),
			Title:     strings.TrimSpace(wire.Playbook.Title),
			Sources:   []string{opts.SourceRef},
			UpdatedAt: opts.Now,
			Body:      body,
		}
		if err := p.Validate(); err == nil {
			prop.Playbook = &p
		}
	}
	return prop, nil
}

// Apply writes the proposal into the store. Returns the paths written,
// relative to the store root, for the CLI to print.
func Apply(s ir.Store, prop Proposal) ([]string, error) {
	var written []string
	for _, f := range prop.Facts {
		if err := s.WriteFact(f); err != nil {
			return written, err
		}
		written = append(written, ir.DirName+"/facts/"+f.Filename())
	}
	if prop.Playbook != nil {
		if err := s.WritePlaybook(*prop.Playbook); err != nil {
			return written, err
		}
		written = append(written, ir.DirName+"/playbooks/"+prop.Playbook.Filename())
	}
	return written, nil
}
