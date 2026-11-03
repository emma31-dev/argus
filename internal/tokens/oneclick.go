package tokens

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/emma31-dev/argus/internal/model"
)

// DefaultOneClickURL is the NEAR Intents 1Click supported-tokens endpoint.
// It was believed correct at the time of writing but is not verified from
// this sandbox; override it with OneClick.URL if it moves.
const DefaultOneClickURL = "https://1click.chaindefuser.com/v0/tokens"

const maxOneClickBody = 16 << 20

// OneClick fetches the supported-tokens list from the 1Click API.
type OneClick struct {
	URL    string       // defaults to DefaultOneClickURL
	Client *http.Client // defaults to http.DefaultClient
}

// Name implements Source.
func (o *OneClick) Name() string { return SourceOneClick }

// Fetch implements Source. It keeps only assets whose assetId parses as
// nep141 or nep245 and skips malformed entries rather than failing.
func (o *OneClick) Fetch(ctx context.Context) ([]model.Token, error) {
	url := o.URL
	if url == "" {
		url = DefaultOneClickURL
	}
	hc := o.Client
	if hc == nil {
		hc = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("1click request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxOneClickBody))
	if err != nil {
		return nil, fmt.Errorf("1click read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("1click: HTTP %d", resp.StatusCode)
	}
	return ParseOneClick(body)
}

// flexInt accepts a JSON number or a numeric string.
type flexInt struct {
	v  int
	ok bool
}

func (f *flexInt) UnmarshalJSON(b []byte) error {
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		var s string
		if err2 := json.Unmarshal(b, &s); err2 != nil {
			return nil // ignore unusable value
		}
		n = json.Number(s)
	}
	i, err := strconv.Atoi(n.String())
	if err != nil {
		return nil
	}
	f.v, f.ok = i, true
	return nil
}

// ParseOneClick parses a 1Click token list body: an array of objects, or an
// object wrapping the array under "tokens" or "data".
func ParseOneClick(body []byte) ([]model.Token, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil {
		var wrapped struct {
			Tokens []json.RawMessage `json:"tokens"`
			Data   []json.RawMessage `json:"data"`
		}
		if err2 := json.Unmarshal(body, &wrapped); err2 != nil {
			return nil, fmt.Errorf("1click: unrecognised response: %w", err)
		}
		items = wrapped.Tokens
		if items == nil {
			items = wrapped.Data
		}
	}
	seen := map[string]bool{}
	var out []model.Token
	for _, raw := range items {
		var it struct {
			AssetID  string  `json:"assetId"`
			Decimals flexInt `json:"decimals"`
			Symbol   string  `json:"symbol"`
		}
		if err := json.Unmarshal(raw, &it); err != nil || it.AssetID == "" {
			continue
		}
		t, err := Parse(it.AssetID)
		if err != nil || !InScope(t) || seen[t.TokenID] {
			continue
		}
		seen[t.TokenID] = true
		t.Symbol, t.Decimals, t.Source = it.Symbol, it.Decimals.v, SourceOneClick
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("1click: no nep141/nep245 assets in response (%d items)", len(items))
	}
	return out, nil
}
