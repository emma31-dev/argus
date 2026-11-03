package events

import (
	"encoding/json"
	"math/big"
	"os"
	"testing"

	"github.com/emma31-dev/argus/internal/model"
)

func loadBlock(t *testing.T, name string) *Block {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var b Block
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	return &b
}

func TestExtract(t *testing.T) {
	b := loadBlock(t, "block_events.json")
	if b.Block.Header.Height != 1000 || b.Block.Header.Hash != "SynthHash1000" || b.Block.Header.Timestamp != 1790000000000000000 {
		t.Fatalf("header %+v", b.Block.Header)
	}
	got := Extract(b, "intents.near")
	// mint x2, token_diff x2, burn x1; failed + other-executor receipts skipped.
	if len(got) != 5 {
		t.Fatalf("got %d rows: %+v", len(got), got)
	}
	first := got[0]
	if first.BlockHeight != 1000 || first.ReceiptID != "SynthReceiptMint" || first.TxHash != "SynthTxMint" ||
		first.Standard != "nep245" || first.Event != "mt_mint" || first.TokenID != "nep141:wrap.near" ||
		first.Amount != "1000000000000000000000000" || first.Raw == "" {
		t.Errorf("first row %+v", first)
	}
	if got[3].Standard != "dip4" || got[3].Event != "token_diff" || got[3].Amount != "-100" || got[3].TokenID != "nep141:wrap.near" {
		t.Errorf("diff row %+v", got[3])
	}
	last := got[4]
	if last.Event != "mt_burn" || last.ReceiptID != "SynthReceiptBurn" || last.TxHash != "SynthTxBurn" {
		t.Errorf("burn row %+v", last)
	}
	for _, r := range got {
		if r.ReceiptID == "SynthReceiptFailed" || r.ReceiptID == "SynthReceiptOther" {
			t.Errorf("row from skipped receipt: %+v", r)
		}
	}
}

func TestExtractExecutorFilterAndDefault(t *testing.T) {
	b := loadBlock(t, "block_events.json")
	if n := len(Extract(b, "other.near")); n != 1 {
		t.Errorf("other.near rows = %d, want 1", n)
	}
	if n := len(Extract(b, "")); n != 5 {
		t.Errorf("default account rows = %d, want 5", n)
	}
	if n := len(Extract(b, "nobody.near")); n != 0 {
		t.Errorf("rows = %d", n)
	}
	if Extract(nil, "intents.near") != nil {
		t.Error("nil block")
	}
}

func TestOutcomeSucceeded(t *testing.T) {
	for s, want := range map[string]bool{
		`{"SuccessValue":""}`: true, `{"SuccessReceiptId":"x"}`: true,
		`{"Failure":{}}`: false, `"Unknown"`: false, ``: false, `null`: false,
	} {
		if got := (Outcome{Status: json.RawMessage(s)}).Succeeded(); got != want {
			t.Errorf("status %s = %v", s, got)
		}
	}
}

func TestExtractNoTokenEvent(t *testing.T) {
	b := &Block{}
	b.Block.Header.Height = 5
	var ro ReceiptOutcome
	ro.ExecutionOutcome.ID = "r1"
	ro.ExecutionOutcome.Outcome = Outcome{ExecutorID: "intents.near", Status: json.RawMessage(`{"SuccessValue":""}`),
		Logs: []string{`EVENT_JSON:{"standard":"dip4","version":"0.3.0","event":"account_locked","data":{"account_id":"a.near"}}`}}
	b.Shards = []Shard{{ReceiptExecutionOutcomes: []ReceiptOutcome{ro}}}
	got := Extract(b, "")
	if len(got) != 1 || got[0].TokenID != "" || got[0].ReceiptID != "r1" || got[0].Event != "account_locked" {
		t.Errorf("%+v", got)
	}
}

func TestDeltasBigInts(t *testing.T) {
	big1 := "340282366920938463463374607431768211455" // u128 max
	rows := []model.IncidentEvent{
		{Standard: "nep245", Event: "mt_mint", TokenID: "a", Amount: big1},
		{Standard: "nep245", Event: "mt_mint", TokenID: "a", Amount: big1},
		{Standard: "nep245", Event: "mt_burn", TokenID: "a", Amount: "5"},
		{Standard: "nep245", Event: "mt_burn", TokenID: "b", Amount: "7"},
		{Standard: "nep245", Event: "mt_transfer", TokenID: "c", Amount: "99"},
		{Standard: "nep245", Event: "mt_mint", TokenID: "a", Amount: "garbage"},
		{Standard: "dip4", Event: "ft_withdraw", TokenID: "a", Amount: "1000"},
	}
	d := Deltas(rows)
	want := new(big.Int)
	want.SetString(big1, 10)
	want.Mul(want, big.NewInt(2))
	want.Sub(want, big.NewInt(5))
	if d["a"].Cmp(want) != 0 {
		t.Errorf("a = %s want %s", d["a"], want)
	}
	if d["b"].Cmp(big.NewInt(-7)) != 0 {
		t.Errorf("b = %s", d["b"])
	}
	if c, ok := d["c"]; !ok || c.Sign() != 0 {
		t.Errorf("c = %v", c)
	}
	if len(d) != 3 {
		t.Errorf("len %d", len(d))
	}
	b := loadBlock(t, "block_events.json")
	fd := Deltas(Extract(b, ""))
	if fd["nep141:wrap.near"].Cmp(new(big.Int).Sub(mustBig("1000000000000000000000000"), big.NewInt(400))) != 0 {
		t.Errorf("fixture delta %s", fd["nep141:wrap.near"])
	}
}

func mustBig(s string) *big.Int { v, _ := new(big.Int).SetString(s, 10); return v }
