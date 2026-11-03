package tokens

import (
	"errors"
	"testing"

	"github.com/emma31-dev/argus/internal/model"
)

func TestParse(t *testing.T) {
	good := []struct {
		id    string
		want  model.Token
		scope bool
	}{
		{"nep141:wrap.near", model.Token{TokenID: "nep141:wrap.near", Standard: model.StandardNEP141, Contract: "wrap.near"}, true},
		{"nep141:17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1",
			model.Token{TokenID: "nep141:17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1", Standard: model.StandardNEP141, Contract: "17208628f84f5d6ad33f0da3bbbeb27ffcb398eac501a31bd6ad2011e36133a1"}, true},
		{"nep245:v2_1.omni.hot.tg:56_11111111111111111111",
			model.Token{TokenID: "nep245:v2_1.omni.hot.tg:56_11111111111111111111", Standard: model.StandardNEP245, Contract: "v2_1.omni.hot.tg", InnerID: "56_11111111111111111111"}, true},
		{"nep245:mt.near:a:b:c", // inner id keeps further colons
			model.Token{TokenID: "nep245:mt.near:a:b:c", Standard: model.StandardNEP245, Contract: "mt.near", InnerID: "a:b:c"}, true},
		{"nep171:nft.near:token-1",
			model.Token{TokenID: "nep171:nft.near:token-1", Standard: model.StandardNEP171, Contract: "nft.near", InnerID: "token-1"}, false},
	}
	for _, tc := range good {
		got, err := Parse(tc.id)
		if err != nil {
			t.Errorf("%s: %v", tc.id, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %+v want %+v", tc.id, got, tc.want)
		}
		if InScope(got) != tc.scope {
			t.Errorf("%s: InScope=%v", tc.id, !tc.scope)
		}
	}
	bad := []struct {
		id  string
		err error
	}{
		{"", ErrInvalidID},
		{"wrap.near", ErrInvalidID},
		{"nep141:", ErrInvalidID},
		{"nep141:Wrap.near", ErrInvalidID},
		{"nep141:wrap near", ErrInvalidID},
		{"nep141:a:b", ErrInvalidID},
		{"nep245:mt.near", ErrInvalidID},
		{"nep245:mt.near:", ErrInvalidID},
		{"nep245::inner", ErrInvalidID},
		{"nep171:nft.near", ErrInvalidID},
		{"imt:minter.near:x", ErrUnsupportedStandard},
		{"erc20:0xabc", ErrUnsupportedStandard},
	}
	for _, tc := range bad {
		if _, err := Parse(tc.id); !errors.Is(err, tc.err) {
			t.Errorf("%q: err %v want %v", tc.id, err, tc.err)
		}
	}
}

func TestHeldMethod(t *testing.T) {
	ft, _ := Parse("nep141:wrap.near")
	c, m, a := HeldMethod(ft)
	if c != "wrap.near" || m != "ft_balance_of" {
		t.Fatalf("%s %s", c, m)
	}
	if a.(map[string]string)["account_id"] != "intents.near" {
		t.Fatalf("args %v", a)
	}
	mt, _ := Parse("nep245:v2_1.omni.hot.tg:56_1")
	c, m, a = HeldMethodFor("custom.near", mt)
	args := a.(map[string]string)
	if c != "v2_1.omni.hot.tg" || m != "mt_balance_of" || args["account_id"] != "custom.near" || args["token_id"] != "56_1" {
		t.Fatalf("%s %s %v", c, m, a)
	}
	nft, _ := Parse("nep171:nft.near:1")
	if c, m, _ := HeldMethod(nft); c != "" || m != "" {
		t.Fatal("nft must have no held method")
	}
}
