package adminops

import (
	"errors"
	"strings"
	"testing"
)

func TestVaultDefinitionPVFChanges(t *testing.T) {
	text := "[required level]\n60\n[upgrade info]\n1 10000000 -1 0 100000 -1\n8 100000000 -1 0 2000000 -1\n240 400000000 -1 0 0 104907\n[/upgrade info]"
	a, e := ParseVaultDefinition(text, strings.Repeat("a", 64))
	if e != nil || len(a.Tiers) != 3 {
		t.Fatal(a, e)
	}
	if v, ok := a.GoldLimit(8); !ok || v != 100000000 {
		t.Fatal(v, ok)
	}
	for _, candidate := range []struct{ text, sha string }{
		{strings.Replace(text, "240 400000000", "248 500000000", 1), strings.Repeat("a", 64)},
		{strings.Replace(text, "240 400000000 -1 0 0 104907\n", "", 1), strings.Repeat("a", 64)},
		{text, strings.Repeat("b", 64)},
	} {
		b, e := ParseVaultDefinition(candidate.text, candidate.sha)
		if e != nil || b.Revision == a.Revision {
			t.Fatal(b, e)
		}
	}
	clamped, e := ParseVaultDefinition(strings.Replace(text, "400000000", "4294967295", 1), "")
	if e != nil || clamped.Tiers[2].GoldLimit != MaxVaultGold {
		t.Fatal(clamped, e)
	}
	for _, bad := range []string{
		"", strings.Repeat("x", 65537), "[upgrade info]\n1 10 -1 0 0 -1",
		strings.Replace(text, "240 400000000", "8 400000000", 1),
		strings.Replace(text, "240 400000000", "32768 400000000", 1),
		strings.Replace(text, "240 400000000", "240 -1", 1),
		strings.Replace(text, "240 400000000", "0 400000000", 1),
		strings.Replace(text, "240 400000000", "240 `400000000`", 1),
		strings.Replace(text, "240 400000000", "240 400000000 0", 1),
		text + "\n[upgrade info]\n1 10 -1 0 0 -1\n[/upgrade info]",
		"[/upgrade info]\n" + strings.Replace(text, "[/upgrade info]", "", 1),
	} {
		if _, e := ParseVaultDefinition(bad, ""); !errors.Is(e, ErrVaultDefinition) {
			t.Fatal("malformed table accepted", e)
		}
	}
}
