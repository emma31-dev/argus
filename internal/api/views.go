package api

import (
	"math/big"
	"regexp"
	"sort"
	"time"

	"github.com/emma31-dev/argus/internal/model"
	"github.com/emma31-dev/argus/internal/tokens"
)

// snapshotView is the "latest" object of a token.
type snapshotView struct {
	BlockHeight uint64    `json:"block_height"`
	BlockTime   time.Time `json:"block_time"`
	Owed        string    `json:"owed"`
	Held        string    `json:"held"`
	Gap         string    `json:"gap"`
	Provider    string    `json:"provider"`
}

func newSnapshotView(s model.Snapshot) *snapshotView {
	return &snapshotView{
		BlockHeight: s.BlockHeight, BlockTime: s.BlockTime.UTC(),
		Owed: s.Owed, Held: s.Held, Gap: s.Gap, Provider: s.Provider,
	}
}

// pointView is one history point.
type pointView struct {
	BlockHeight uint64    `json:"block_height"`
	BlockTime   time.Time `json:"block_time"`
	Owed        string    `json:"owed"`
	Held        string    `json:"held"`
	Gap         string    `json:"gap"`
	Provider    string    `json:"provider,omitempty"`
}

func newPoints(snaps []model.Snapshot) []pointView {
	out := make([]pointView, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, pointView{s.BlockHeight, s.BlockTime.UTC(), s.Owed, s.Held, s.Gap, s.Provider})
	}
	return out
}

// tokenView is one element of /v1/tokens.
type tokenView struct {
	ID             string        `json:"id"`
	Standard       string        `json:"standard"`
	Contract       string        `json:"contract"`
	InnerID        string        `json:"inner_id"`
	Symbol         string        `json:"symbol"`
	Chain          string        `json:"chain"`
	Decimals       int           `json:"decimals"`
	Status         model.Status  `json:"status"`
	NegativeStreak int           `json:"negative_streak"`
	Stale          bool          `json:"stale"`
	LastError      string        `json:"last_error"`
	Latest         *snapshotView `json:"latest"`
}

// tokenDetailView is /v1/tokens/{id}.
type tokenDetailView struct {
	tokenView
	OpenIncident *incidentView `json:"open_incident"`
}

// chainLabel prefers the stored label and derives one from the id otherwise.
func chainLabel(t model.Token) string {
	if t.Chain != "" {
		return t.Chain
	}
	return tokens.ChainOf(t.TokenID)
}

// buildToken merges token metadata, alert state and the latest snapshot. A
// token without state or without a snapshot is "unknown".
func buildToken(t model.Token, st *model.TokenState, snap *model.Snapshot) tokenView {
	v := tokenView{
		ID: t.TokenID, Standard: string(t.Standard), Contract: t.Contract, InnerID: t.InnerID,
		Symbol: t.Symbol, Chain: chainLabel(t), Decimals: t.Decimals, Status: model.StatusUnknown,
	}
	if snap != nil {
		v.Latest = newSnapshotView(*snap)
	}
	if st != nil {
		v.NegativeStreak = st.NegativeStreak
		v.Stale = st.Stale
		v.LastError = sanitizeError(st.LastError)
		if snap != nil {
			switch st.Status {
			case model.StatusGreen, model.StatusAmber, model.StatusRed:
				v.Status = st.Status
			}
		}
	}
	return v
}

var urlInText = regexp.MustCompile(`https?://[^\s"'<>]+`)

// sanitizeError strips paths, queries and credentials from URLs inside an
// error message: provider URLs may embed API keys.
func sanitizeError(s string) string {
	return urlInText.ReplaceAllStringFunc(s, func(m string) string { return redactURL(m) })
}

func statusRank(s model.Status) int {
	switch s {
	case model.StatusRed:
		return 0
	case model.StatusAmber:
		return 1
	case model.StatusUnknown:
		return 2
	default:
		return 3
	}
}

// ratioKey orders gap/owed: class -1 is -infinity (deficit with nothing
// owed), 1 is +infinity (no data, or a zero-owed token without deficit).
type ratioKey struct {
	class int
	r     *big.Rat
}

func (a ratioKey) less(b ratioKey) bool {
	if a.class != b.class {
		return a.class < b.class
	}
	if a.class != 0 {
		return false
	}
	return a.r.Cmp(b.r) < 0
}

func riskRatio(v tokenView) ratioKey {
	if v.Latest == nil {
		return ratioKey{class: 1}
	}
	gap, ok1 := new(big.Int).SetString(v.Latest.Gap, 10)
	owed, ok2 := new(big.Int).SetString(v.Latest.Owed, 10)
	if !ok1 || !ok2 {
		return ratioKey{class: 1}
	}
	if owed.Sign() == 0 {
		if gap.Sign() < 0 {
			return ratioKey{class: -1}
		}
		return ratioKey{class: 1}
	}
	return ratioKey{r: new(big.Rat).SetFrac(gap, owed)}
}

// sortTokens orders by deficit risk: red, amber, unknown, green; within a
// status by gap/owed ascending (most negative first); ties by symbol, id.
func sortTokens(ts []tokenView) {
	keys := make(map[string]ratioKey, len(ts))
	for _, t := range ts {
		keys[t.ID] = riskRatio(t)
	}
	sort.SliceStable(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if ra, rb := statusRank(a.Status), statusRank(b.Status); ra != rb {
			return ra < rb
		}
		ka, kb := keys[a.ID], keys[b.ID]
		if ka.less(kb) {
			return true
		}
		if kb.less(ka) {
			return false
		}
		if a.Symbol != b.Symbol {
			return a.Symbol < b.Symbol
		}
		return a.ID < b.ID
	})
}

// worstStatus is the overall status: the riskiest token status. With no
// tokens it is unknown.
func worstStatus(counts map[model.Status]int) model.Status {
	for _, s := range []model.Status{model.StatusRed, model.StatusAmber, model.StatusUnknown} {
		if counts[s] > 0 {
			return s
		}
	}
	if counts[model.StatusGreen] > 0 {
		return model.StatusGreen
	}
	return model.StatusUnknown
}

// incidentView is the public shape of an incident.
type incidentView struct {
	ID              int64      `json:"id"`
	TokenID         string     `json:"token_id"`
	Symbol          string     `json:"symbol"`
	Chain           string     `json:"chain"`
	Decimals        int        `json:"decimals"`
	StartBlock      uint64     `json:"start_block"`
	RedBlock        uint64     `json:"red_block"`
	EndBlock        *uint64    `json:"end_block"`
	OpenedAt        time.Time  `json:"opened_at"`
	ClosedAt        *time.Time `json:"closed_at"`
	DurationSeconds *int64     `json:"duration_seconds"`
	MaxDeficit      string     `json:"max_deficit"`
	MaxDeficitBlock uint64     `json:"max_deficit_block"`
	Summary         string     `json:"summary"`
}

func newIncidentView(i model.Incident, meta map[string]model.Token) incidentView {
	v := incidentView{
		ID: i.ID, TokenID: i.TokenID, Symbol: i.TokenID,
		StartBlock: i.StartBlock, RedBlock: i.RedBlock, OpenedAt: i.OpenedAt.UTC(),
		MaxDeficit: i.MaxDeficit, MaxDeficitBlock: i.MaxDeficitBlock, Summary: i.Summary,
		Chain: tokens.ChainOf(i.TokenID),
	}
	if t, ok := meta[i.TokenID]; ok {
		v.Symbol, v.Decimals, v.Chain = t.Symbol, t.Decimals, chainLabel(t)
	}
	if i.ClosedAt != nil {
		c := i.ClosedAt.UTC()
		v.ClosedAt = &c
		d := int64(c.Sub(i.OpenedAt).Seconds())
		v.DurationSeconds = &d
		end := i.EndBlock
		v.EndBlock = &end
	}
	return v
}
