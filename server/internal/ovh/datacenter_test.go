package ovh

import "testing"

func TestConvertDisplayDCKeepsFrontendAliases(t *testing.T) {
	for input, want := range map[string]string{
		"": "gra", " GRA ": "gra", "MUM": "ynm", "ynm": "ynm",
		"tor": "ca-east-tor-a", "par-a": "eu-west-par-a",
		"par-b": "eu-west-par-b", "par-c": "eu-west-par-c",
		"ca-east-tor-a": "ca-east-tor-a", "eu-west-par-b": "eu-west-par-b",
	} {
		if got := ConvertDisplayDCToAPIDC(input); got != want {
			t.Errorf("%q: got %q, want %q", input, got, want)
		}
	}
}
