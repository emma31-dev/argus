// Package rpctest replays recorded NEAR JSON-RPC exchanges.
//
// Exchanges are the files written by rpc.WithRecorder (see
// cmd/probe and cmd/verify -capture):
//
//	{"provider": "...", "request": {<JSON-RPC request>}, "response": {<JSON-RPC response>}}
//
// A Replay is an http.RoundTripper. A request is matched by what determines
// the answer: for "query" calls (request_type, account_id, method_name,
// args_base64, block_id or finality); for "block" calls (block_id or
// finality). Anything else is a miss: the round trip fails with an error that
// names the missing request, and the miss is recorded, so a fixture that
// does not cover what the code asks for fails loudly instead of silently
// returning something plausible.
//
// Requests by block_id are answered from a fixed map. Requests by finality
// ("the latest final block") change over time, so every recording of one is
// kept in file-name order and handed out one per request; this is how a
// fixture holds several rounds.
package rpctest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Replay serves recorded exchanges. It is safe for concurrent use.
type Replay struct {
	mu        sync.Mutex
	byKey     map[string][]byte   // block_id-addressed exchanges
	sequences map[string][][]byte // finality-addressed exchanges, in order
	next      map[string]int
	providers []string
	misses    []string
	files     int
}

// Load reads every *.json file under dir (recursively, in lexical order)
// that has the recorder format. Other JSON files (expected.json,
// fixture.json) are ignored. It is an error if no exchange is found.
func Load(dir string) (*Replay, error) {
	r := &Replay{byKey: map[string][]byte{}, sequences: map[string][][]byte{}, next: map[string]int{}}
	seenProvider := map[string]bool{}
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".json") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var ex struct {
			Provider string          `json:"provider"`
			Request  json.RawMessage `json:"request"`
			Response json.RawMessage `json:"response"`
		}
		if json.Unmarshal(raw, &ex) != nil || len(ex.Request) == 0 || len(ex.Response) == 0 {
			continue // not a recorded exchange
		}
		key, finality, err := KeyOf(ex.Request)
		if err != nil {
			return nil, fmt.Errorf("rpctest: %s: %w", p, err)
		}
		body := responseBody(ex.Response)
		if finality {
			r.sequences[key] = append(r.sequences[key], body)
		} else if _, dup := r.byKey[key]; !dup {
			r.byKey[key] = body
		}
		if ex.Provider != "" && !seenProvider[ex.Provider] {
			seenProvider[ex.Provider] = true
			r.providers = append(r.providers, ex.Provider)
		}
		r.files++
	}
	if r.files == 0 {
		return nil, fmt.Errorf("rpctest: no recorded exchanges under %s", dir)
	}
	return r, nil
}

// responseBody returns the HTTP body for a recorded response: JSON as is, a
// JSON string (a non-JSON body the recorder had to quote) unquoted.
func responseBody(resp json.RawMessage) []byte {
	var s string
	if json.Unmarshal(resp, &s) == nil {
		return []byte(s)
	}
	return append([]byte(nil), resp...)
}

// Exchanges is the number of recorded exchanges.
func (r *Replay) Exchanges() int { return r.files }

// Providers lists the provider names found in the recordings.
func (r *Replay) Providers() []string { return r.providers }

// FinalBlocks is the number of recorded "latest final block" requests, i.e.
// the number of rounds the recording can serve.
func (r *Replay) FinalBlocks() int {
	return len(r.sequences[blockKey("finality", "final")])
}

// Misses returns a description of every request that was not recorded.
func (r *Replay) Misses() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.misses...)
}

func blockKey(kind, v string) string { return "block|" + kind + "=" + v }

// KeyOf computes the match key of a JSON-RPC request body and reports
// whether it is addressed by finality (a sequence) rather than block id.
func KeyOf(body []byte) (key string, finality bool, err error) {
	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return "", false, fmt.Errorf("undecodable request: %w", err)
	}
	var p struct {
		RequestType string          `json:"request_type"`
		AccountID   string          `json:"account_id"`
		MethodName  string          `json:"method_name"`
		ArgsBase64  string          `json:"args_base64"`
		BlockID     json.RawMessage `json:"block_id"`
		Finality    string          `json:"finality"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return "", false, fmt.Errorf("params of %q are not an object: %w", req.Method, err)
		}
	}
	var at string
	switch {
	case p.Finality != "":
		at, finality = "finality="+p.Finality, true
	case len(p.BlockID) > 0:
		at = "block_id=" + strings.Trim(string(p.BlockID), `"`)
	default:
		return "", false, fmt.Errorf("%s request has neither block_id nor finality", req.Method)
	}
	switch req.Method {
	case "block":
		return "block|" + at, finality, nil
	case "query":
		return fmt.Sprintf("query|%s|%s|%s|%s|%s", p.RequestType, p.AccountID, p.MethodName, p.ArgsBase64, at), finality, nil
	}
	return "", false, fmt.Errorf("unsupported method %q", req.Method)
}

// RoundTrip implements http.RoundTripper.
func (r *Replay) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		if body, err = io.ReadAll(req.Body); err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}
	key, finality, err := KeyOf(body)
	if err != nil {
		return nil, r.miss(fmt.Sprintf("unreadable request (%v): %s", err, trunc(string(body))))
	}
	r.mu.Lock()
	var resp []byte
	if finality {
		seq := r.sequences[key]
		if n := r.next[key]; n < len(seq) {
			resp = seq[n]
			r.next[key] = n + 1
		}
	} else {
		resp = r.byKey[key]
	}
	r.mu.Unlock()
	if resp == nil {
		return nil, r.miss(key)
	}
	status := http.StatusOK
	if !json.Valid(resp) {
		status = http.StatusInternalServerError
	}
	return &http.Response{
		StatusCode:    status,
		Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(resp)),
		ContentLength: int64(len(resp)),
		Request:       req,
	}, nil
}

// ErrNotRecorded is wrapped by the error returned for an unmatched request.
var ErrNotRecorded = errors.New("rpctest: request not in the recorded fixture")

func (r *Replay) miss(desc string) error {
	r.mu.Lock()
	r.misses = append(r.misses, desc)
	r.mu.Unlock()
	return fmt.Errorf("%w: %s", ErrNotRecorded, desc)
}

func trunc(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
