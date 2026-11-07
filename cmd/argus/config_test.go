package main

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadConfigDefaults(t *testing.T) {
	c, err := LoadConfig(env(map[string]string{
		"DATABASE_URL": "postgres://x", "ARGUS_RPC_URLS": "https://a.example/key, https://b.example",
	}), true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Interval != 30*time.Second || c.K != 4 || c.BatchSize != 50 || c.Concurrency != 8 || c.HTTPAddr != ":8080" ||
		c.RPCRPS != DefaultRPCRPS || c.APIRateLimit != 60 || c.DiscoveryInterval != 24*time.Hour || c.Retention != 720*time.Hour ||
		len(c.RPCURLs) != 2 || c.IntentsAccount != "intents.near" || len(c.CORSOrigins) != 1 || c.CORSOrigins[0] != "*" {
		t.Fatalf("defaults = %+v", c)
	}
}

func TestLoadConfigReportsEveryProblem(t *testing.T) {
	_, err := LoadConfig(env(map[string]string{
		"ARGUS_INTERVAL": "fast", "ARGUS_DEBOUNCE_K": "0", "ARGUS_RPC_URLS": "ftp://x, https://rpc.example/SECRETKEY?x",
		"ARGUS_TRUST_PROXY": "maybe", "ARGUS_LOG_LEVEL": "loud", "LLM_ENABLED": "true", "ARGUS_RPC_RPS": "-1",
		"ARGUS_PUBLIC_URL": "dashboard",
	}), true)
	if err == nil {
		t.Fatal("expected errors")
	}
	msg := err.Error()
	for _, want := range []string{"ARGUS_INTERVAL", "ARGUS_DEBOUNCE_K", "ftp://x", "ARGUS_TRUST_PROXY", "ARGUS_LOG_LEVEL", "LLM_BASE_URL", "ARGUS_RPC_RPS", "DATABASE_URL", "ARGUS_PUBLIC_URL"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "SECRETKEY") {
		t.Errorf("api key leaked into config error:\n%s", msg)
	}
}

func TestOnceDoesNotNeedDB(t *testing.T) {
	if _, err := LoadConfig(env(map[string]string{"ARGUS_RPC_URLS": "https://a.example"}), false); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(env(nil), false)
	if err != nil || len(c.RPCURLs) != 4 || c.RPCURLs[0] != "https://free.rpc.fastnear.com" {
		t.Fatalf("default providers = %v err %v", c.RPCURLs, err)
	}
	for _, u := range c.RPCURLs {
		if strings.Contains(u, "near.org") {
			t.Errorf("deprecated NEAR-hosted endpoint in defaults: %s", u)
		}
	}
	if _, err := LoadConfig(env(map[string]string{"ARGUS_RPC_URLS": " , "}), false); err != nil {
		t.Fatalf("blank list falls back to defaults: %v", err)
	}
}

func TestProviderNames(t *testing.T) {
	got := providerNames([]string{"https://rpc.mainnet.fastnear.com", "https://rpc.mainnet.near.org/x", "https://rpc.mainnet.fastnear.com/other"})
	if strings.Join(got, ",") != "rpc.mainnet.fastnear.com,rpc.mainnet.near.org,rpc.mainnet.fastnear.com#2" {
		t.Fatalf("names = %v", got)
	}
}

func TestParseThresholds(t *testing.T) {
	th, err := parseThresholds(" nep141:wrap.near=1000000000000000000000000 , *=5 ")
	if err != nil {
		t.Fatal(err)
	}
	if th["nep141:wrap.near"].String() != "1000000000000000000000000" || th["*"].String() != "5" {
		t.Fatalf("got %v", th)
	}
	ac := Config{K: 4, RedThresholds: th}.alertConfig()
	if ac.K != 4 || ac.DefaultAbsThreshold.String() != "5" || ac.AbsThreshold["nep141:wrap.near"] == nil {
		t.Fatalf("alert config %+v", ac)
	}
	for _, bad := range []string{"x", "nep141:a=", "=5", "nep141:a=-1", "nep141:a=1.5", "nep141:a=0"} {
		if _, err := parseThresholds(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
	if th, err := parseThresholds(""); err != nil || len(th) != 0 {
		t.Fatalf("empty: %v %v", th, err)
	}
}
