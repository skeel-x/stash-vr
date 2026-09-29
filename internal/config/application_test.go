package config

import (
	"slices"
	"testing"

	"github.com/spf13/pflag"
)

// registerTestFlags registers the settings on a fresh FlagSet and returns it
// with the keys that were bound, without touching the process flags or viper.
func registerTestFlags(t *testing.T) (*pflag.FlagSet, []string) {
	t.Helper()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	var bound []string
	registerFlags(fs, func(key string, flag *pflag.Flag) error {
		if flag == nil {
			t.Errorf("bind(%s) got a nil flag: bound before declared", key)
		}
		bound = append(bound, key)
		return nil
	})
	return fs, bound
}

func TestRegisterFlags_TogglesAreBools(t *testing.T) {
	fs, _ := registerTestFlags(t)
	toggles := []struct {
		key string
		def string
	}{
		{envKeyStashTLSInsecure, "false"},
		{envKeyDisableLogColor, "false"},
		{envKeyDisableRedact, "false"},
		{envKeyForceHTTPS, "false"},
		{envKeyDeovrAutoload, "true"},
		{envKeyPerformerFacets, "true"},
		{envKeyDateLookup, "true"},
		{envKeyDateWriteback, "false"},
		{envKeyGenerateSummaryIds, "false"},
		{envKeyCoverBadgeQuality, "true"},
		{envKeyCoverBadgeFormat, "false"},
		{envKeyCoverBadgePass, "true"},
		{envKeyCoverBadgeDuration, "true"},
		{envKeyCoverBadgeRate, "true"},
		{envKeyLearnStudio, "false"},
		{envKeyCorrectVertical, "true"},
		{"help", "false"},
	}
	for _, tt := range toggles {
		t.Run(tt.key, func(t *testing.T) {
			f := fs.Lookup(tt.key)
			if f == nil {
				t.Fatalf("flag %s not declared", tt.key)
			}
			if got := f.Value.Type(); got != "bool" {
				t.Fatalf("flag %s has type %s, want bool", tt.key, got)
			}
			if f.DefValue != tt.def {
				t.Fatalf("flag %s defaults to %q, want %q", tt.key, f.DefValue, tt.def)
			}
		})
	}
}

func TestRegisterFlags_SummaryIdsParsesAsBool(t *testing.T) {
	// A bool flag takes --GENERATE_SUMMARY_IDS with no value, which a string
	// flag would reject.
	fs, _ := registerTestFlags(t)
	if err := fs.Parse([]string{"--" + envKeyGenerateSummaryIds}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := fs.GetBool(envKeyGenerateSummaryIds)
	if err != nil {
		t.Fatalf("GetBool: %v", err)
	}
	if !got {
		t.Fatal("flag given without a value should be true")
	}
}

func TestRegisterFlags_BindsEveryDeclaredFlag(t *testing.T) {
	fs, bound := registerTestFlags(t)
	var declared []string
	fs.VisitAll(func(f *pflag.Flag) { declared = append(declared, f.Name) })
	slices.Sort(declared)
	slices.Sort(bound)
	if !slices.Equal(declared, bound) {
		t.Fatalf("declared %v\nbound    %v", declared, bound)
	}
}
