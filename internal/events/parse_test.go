package events

import (
	"reflect"
	"testing"
)

// Log lines below are built from the serde definitions, not captured from
// mainnet:
//   NEP-245: /home/user/near/intents/crates/near/nep245/src/events.rs
//            (tag="event", content="data", snake_case; amounts DisplayFromStr;
//            memo/authorized_id omitted when None)
//   DIP-4:   /home/user/near/intents/contracts/defuse/core/src/events/mod.rs
//            and contracts/defuse/core/src/intents/{tokens,account}.rs

func mustParse(t *testing.T, log string) Event {
	t.Helper()
	ev, ok, err := ParseLog(log)
	if err != nil || !ok {
		t.Fatalf("ParseLog(%q) ok=%v err=%v", log, ok, err)
	}
	return ev
}

func TestParseNEP245(t *testing.T) {
	cases := []struct {
		name, log string
		want      []TokenAmount
	}{
		{"mint", `EVENT_JSON:{"standard":"nep245","version":"1.0.0","event":"mt_mint","data":[{"owner_id":"alice.near","token_ids":["nep141:wrap.near","nep141:a.near"],"amounts":["100","200"],"memo":"deposit"}]}`,
			[]TokenAmount{{"nep141:wrap.near", "100", "alice.near", "", "mint"}, {"nep141:a.near", "200", "alice.near", "", "mint"}}},
		{"burn", `EVENT_JSON:{"standard":"nep245","version":"1.0.0","event":"mt_burn","data":[{"owner_id":"bob.near","authorized_id":"x.near","token_ids":["nep141:wrap.near"],"amounts":["340282366920938463463374607431768211455"]}]}`,
			[]TokenAmount{{"nep141:wrap.near", "340282366920938463463374607431768211455", "bob.near", "", "burn"}}},
		{"transfer", `EVENT_JSON:{"standard":"nep245","version":"1.0.0","event":"mt_transfer","data":[{"old_owner_id":"a.near","new_owner_id":"b.near","token_ids":["t"],"amounts":["1"],"memo":"hi"}]}`,
			[]TokenAmount{{"t", "1", "a.near", "b.near", "transfer"}}},
	}
	for _, c := range cases {
		ev := mustParse(t, c.log)
		if ev.Standard != "nep245" || ev.Version != "1.0.0" {
			t.Errorf("%s: envelope %+v", c.name, ev)
		}
		if got := ev.TokenAmounts(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
		if ev.Raw != c.log[len(LogPrefix):] {
			t.Errorf("%s: raw mismatch", c.name)
		}
	}
	if ev := mustParse(t, cases[1].log); ev.Items[0].AuthorizedID != "x.near" {
		t.Error("authorized_id lost")
	}
}

func TestParseDIP4(t *testing.T) {
	p := func(ver, event, data string) string {
		return `EVENT_JSON:{"standard":"dip4","version":"` + ver + `","event":"` + event + `","data":` + data + `}`
	}
	cases := []struct {
		name string
		log  string
		want []TokenAmount
	}{
		{"transfer", p("0.4.3", "transfer", `[{"intent_hash":"h","account_id":"alice.near","receiver_id":"bob.near","tokens":{"nep141:b":"2","nep141:a":"1"},"memo":"m"}]`),
			[]TokenAmount{{"nep141:a", "1", "alice.near", "bob.near", "transfer"}, {"nep141:b", "2", "alice.near", "bob.near", "transfer"}}},
		{"token_diff", p("0.3.0", "token_diff", `[{"intent_hash":"h","account_id":"alice.near","diff":{"nep141:token.near":"100","nep245:token.near:abcd":"-200"},"referral":"r","fees_collected":{"nep141:token.near":"1"}}]`),
			[]TokenAmount{{"nep141:token.near", "100", "alice.near", "", "diff"}, {"nep245:token.near:abcd", "-200", "alice.near", "", "diff"}}},
		{"intents_executed", p("0.3.1", "intents_executed", `[{"intent_hash":"h","account_id":"alice.near","nonce":"AAAA"}]`), nil},
		{"ft_withdraw", p("0.4.3", "ft_withdraw", `[{"intent_hash":"h","account_id":"alice.near","token":"token.near","receiver_id":"bob.near","amount":"100","memo":"m","msg":"x","storage_deposit":"100","min_gas":"10000000000000"}]`),
			[]TokenAmount{{"nep141:token.near", "100", "alice.near", "bob.near", "withdraw"}}},
		{"ft_withdraw numeric gas", p("0.4.3", "ft_withdraw", `[{"account_id":"a.near","token":"t.near","receiver_id":"b.near","amount":"1","min_gas":10000}]`),
			[]TokenAmount{{"nep141:t.near", "1", "a.near", "b.near", "withdraw"}}},
		{"nft_withdraw", p("0.4.3", "nft_withdraw", `[{"account_id":"a.near","token":"nft.near","receiver_id":"b.near","token_id":"7"}]`),
			[]TokenAmount{{"nep171:nft.near:7", "1", "a.near", "b.near", "withdraw"}}},
		{"mt_withdraw", p("0.4.3", "mt_withdraw", `[{"intent_hash":"h","account_id":"alice.near","token":"mt.near","receiver_id":"bob.near","token_ids":["x","y"],"amounts":["1","2"]}]`),
			[]TokenAmount{{"nep245:mt.near:x", "1", "alice.near", "bob.near", "withdraw"}, {"nep245:mt.near:y", "2", "alice.near", "bob.near", "withdraw"}}},
		{"native_withdraw", p("0.4.3", "native_withdraw", `[{"account_id":"a.near","receiver_id":"b.near","amount":"100000000000000000000000000"}]`),
			[]TokenAmount{{"native", "100000000000000000000000000", "a.near", "b.near", "withdraw"}}},
		{"storage_deposit", p("0.4.3", "storage_deposit", `[{"account_id":"a.near","contract_id":"c.near","deposit_for_account_id":"d.near","amount":"5"}]`), nil},
		{"public_key_added", p("0.4.3", "public_key_added", `[{"account_id":"a.near","public_key":"ed25519:xyz"}]`), nil},
		{"account_locked", p("0.3.0", "account_locked", `{"account_id":"a.near"}`), nil},
		{"fee_changed", p("0.3.0", "fee_changed", `{"old_fee":1,"new_fee":2}`), nil},
		{"salt_rotation", p("0.4.0", "salt_rotation", `{"current":"00","invalidated":["01"]}`), nil},
		{"unknown future event", p("9.9.9", "brand_new", `[{"anything":[1,2,3]}]`), nil},
	}
	for _, c := range cases {
		ev := mustParse(t, c.log)
		if ev.Standard != "dip4" {
			t.Errorf("%s: standard %q", c.name, ev.Standard)
		}
		if got := ev.TokenAmounts(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v want %+v", c.name, got, c.want)
		}
		for _, it := range ev.Items {
			if len(it.Raw) == 0 {
				t.Errorf("%s: item without raw", c.name)
			}
		}
	}
	ev := mustParse(t, cases[3].log)
	if it := ev.Items[0]; it.StorageDeposit != "100" || it.MinGas != "10000000000000" || it.Msg != "x" {
		t.Errorf("ft_withdraw fields: %+v", it)
	}
	ev = mustParse(t, cases[8].log)
	if it := ev.Items[0]; it.ContractID != "c.near" || it.DepositForAccountID != "d.near" {
		t.Errorf("storage_deposit fields: %+v", it)
	}
	ev = mustParse(t, cases[2].log)
	if ev.Items[0].Nonce != "AAAA" || ev.Version != "0.3.1" {
		t.Errorf("intents_executed: %+v", ev)
	}
	if len(mustParse(t, cases[12].log).Items) != 1 {
		t.Error("single-object data should become one item")
	}
}

func TestParseNonEvents(t *testing.T) {
	for _, l := range []string{
		"", "hello", "EVENT_JSON", "event_json:{}",
		`EVENT_JSON:{"standard":"nep141","version":"1.0.0","event":"ft_transfer","data":[]}`,
		`EVENT_JSON:{"version":"1.0.0"}`,
	} {
		ev, ok, err := ParseLog(l)
		if ok || err != nil {
			t.Errorf("ParseLog(%q) = %+v ok=%v err=%v; want ignored", l, ev, ok, err)
		}
	}
}

func TestParseMalformed(t *testing.T) {
	for _, l := range []string{
		`EVENT_JSON:{not json`,
		`EVENT_JSON:`,
		`EVENT_JSON:{"standard":"nep245","version":"1","event":"mt_mint","data":"nope"}`,
		`EVENT_JSON:{"standard":"nep245","version":"1","event":"mt_mint","data":[{"owner_id":"a","token_ids":["t"],"amounts":["1","2"]}]}`,
		`EVENT_JSON:{"standard":"nep245","version":"1","event":"mt_mint","data":[{"owner_id":5,"token_ids":["t"],"amounts":["1"]}]}`,
		`EVENT_JSON:{"standard":"dip4","version":"0.4.3","data":[]}`,
	} {
		if _, ok, err := ParseLog(l); err == nil || ok {
			t.Errorf("ParseLog(%q) ok=%v err=%v; want error", l, ok, err)
		}
	}
}

func TestMentions(t *testing.T) {
	ev := mustParse(t, `EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"token_diff","data":[{"account_id":"a","diff":{"nep141:x":"1"},"fees_collected":{"nep141:y":"1"}}]}`)
	if !ev.Mentions("nep141:x") || !ev.Mentions("nep141:y") || ev.Mentions("nep141:z") {
		t.Error("Mentions wrong")
	}
	ev = mustParse(t, `EVENT_JSON:{"standard":"dip4","version":"0.4.3","event":"intents_executed","data":[{"account_id":"a","nonce":"n"}]}`)
	if ev.Mentions("nep141:x") {
		t.Error("unexpected mention")
	}
}
