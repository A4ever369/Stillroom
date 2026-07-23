package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/A4ever369/Stillroom/internal/pack"
)

// Storage is a directory of files, deliberately.
//
// Unlike stillroomd — which owns no source of truth and can be rebuilt from
// git — the hub *is* the only copy of a published pack once the publisher's
// machine moves on. That makes durability the requirement and cleverness the
// enemy: packs are immutable JSON documents written once, named by content
// hash, and a directory of those is something an operator can back up, inspect
// and move with tools they already have.

// Record is the hub's own metadata about a pack. The pack itself is immutable
// and content-addressed; everything that can change about it lives here.
type Record struct {
	ID          string    `json:"id"`
	Publisher   string    `json:"publisher"`
	Mode        string    `json:"mode"`
	Note        string    `json:"note,omitempty"`
	Repo        string    `json:"repo,omitempty"`
	Facts       int       `json:"facts"`
	Playbooks   int       `json:"playbooks"`
	Sessions    int       `json:"sessions"`
	Bytes       int       `json:"bytes"`
	CreatedAt   time.Time `json:"created_at"`
	Downloads   int       `json:"downloads"`
	Revoked     bool      `json:"revoked,omitempty"`
	RevokedAt   time.Time `json:"revoked_at,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Redactions  int       `json:"redactions,omitempty"`
	PublisherID string    `json:"publisher_id,omitempty"`
	// RevokeHash is the SHA-256 of the revoke token handed to the publisher
	// once, at publish time. The token itself is never stored: sharing works
	// without an account, so this is often the only way to take a link back,
	// and a leaked metadata directory must not hand that power to whoever
	// reads it.
	RevokeHash string `json:"revoke_hash,omitempty"`
}

// Gone reports whether a record should be treated as no longer available.
// Revocation and expiry are the publisher's two ways of taking something back;
// both are honoured before the bytes are ever read from disk.
func (r Record) Gone() bool {
	if r.Revoked {
		return true
	}
	return !r.ExpiresAt.IsZero() && time.Now().After(r.ExpiresAt)
}

type Store struct {
	root string
	mu   sync.Mutex // serializes metadata read-modify-write (download counts, revocation)
}

func NewStore(root string) (*Store, error) {
	for _, d := range []string{"packs", "meta"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return nil, err
		}
	}
	return &Store{root: root}, nil
}

func (s *Store) packPath(id string) string { return filepath.Join(s.root, "packs", id+".json") }
func (s *Store) metaPath(id string) string { return filepath.Join(s.root, "meta", id+".json") }

// validID guards every path built from user input. IDs are hex from a content
// hash; anything else is someone probing for a traversal.
func validID(id string) bool {
	if len(id) < 6 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// NewRevokeToken mints the secret that lets an anonymous publisher take a link
// back. Returned to the caller exactly once and never persisted in the clear.
func NewRevokeToken() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		panic("hub: no entropy for a revoke token: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// Put stores a pack and, for a genuinely new one, mints a revoke token. An
// already-known pack returns an empty token: content addressing makes
// publishing idempotent, and re-publishing must not quietly invalidate the
// token the publisher already wrote down.
func (s *Store) Put(p pack.Pack, raw []byte) (Record, string, error) {
	id := p.ID()
	if !validID(id) {
		return Record{}, "", errors.New("hub: refusing a pack with an unusable id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if rec, err := s.load(id); err == nil {
		return rec, "", nil
	}
	if err := os.WriteFile(s.packPath(id), raw, 0o644); err != nil {
		return Record{}, "", err
	}
	token := NewRevokeToken()
	rec := Record{
		ID: id, Publisher: p.Publisher, Mode: string(p.Mode), Note: p.Note,
		Repo: p.Origin.Repo, Facts: len(p.Facts), Playbooks: len(p.Playbooks),
		Sessions: len(p.Sessions), Bytes: len(raw), CreatedAt: time.Now().UTC(),
		Redactions: p.Redactions(), RevokeHash: hashToken(token),
	}
	return rec, token, s.save(rec)
}

// Get returns the record and the pack bytes, or a not-found error for anything
// revoked or expired. Callers never see the difference between "never existed"
// and "taken back" — that distinction is the publisher's business.
func (s *Store) Get(id string) (Record, []byte, error) {
	if !validID(id) {
		return Record{}, nil, os.ErrNotExist
	}
	rec, err := s.load(id)
	if err != nil {
		return Record{}, nil, err
	}
	if rec.Gone() {
		return Record{}, nil, os.ErrNotExist
	}
	raw, err := os.ReadFile(s.packPath(id))
	if err != nil {
		return Record{}, nil, err
	}
	return rec, raw, nil
}

// CountDownload records a fetch. Best-effort: a failed counter update must
// never fail the download itself.
func (s *Store) CountDownload(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(id)
	if err != nil {
		return
	}
	rec.Downloads++
	_ = s.save(rec)
}

// Revoke is how a publisher takes a link back, proving ownership either with
// the account that published it or with the revoke token handed out at publish
// time. The bytes stay on disk so the act is auditable, but the pack stops
// being served immediately.
//
// Neither proof alone is enough to be sloppy about: the token comparison is
// constant-time, because a timing oracle on a revoke endpoint is a way to
// silently delete other people's links.
func (s *Store) Revoke(id, publisherID, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(id)
	if err != nil {
		return err
	}
	byAccount := publisherID != "" && rec.PublisherID == publisherID
	byToken := token != "" && rec.RevokeHash != "" &&
		subtle.ConstantTimeCompare([]byte(hashToken(token)), []byte(rec.RevokeHash)) == 1
	if !byAccount && !byToken {
		return errors.New("hub: not yours to revoke")
	}
	rec.Revoked, rec.RevokedAt = true, time.Now().UTC()
	return s.save(rec)
}

// ByPublisher lists one account's packs, newest first.
func (s *Store) ByPublisher(publisherID string) []Record {
	var out []Record
	if publisherID == "" {
		return out
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "meta"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		rec, err := s.load(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil || rec.PublisherID != publisherID {
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *Store) load(id string) (Record, error) {
	raw, err := os.ReadFile(s.metaPath(id))
	if err != nil {
		return Record{}, err
	}
	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Record{}, fmt.Errorf("hub: unreadable record %s: %w", id, err)
	}
	return rec, nil
}

func (s *Store) save(rec Record) error {
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	// Write-then-rename: a crash mid-write must not leave a half-written record
	// that makes a pack permanently unreadable.
	tmp := s.metaPath(rec.ID) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.metaPath(rec.ID))
}
