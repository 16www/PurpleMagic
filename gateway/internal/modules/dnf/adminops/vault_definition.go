package adminops

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"

	"dnf90.local/gateway/internal/modules/dnf/pvf"
)

var ErrVaultDefinition = errors.New("invalid vault PVF definition")
var ErrVaultDefinitionChanged = errors.New("vault PVF definition changed")

const VaultDefinitionPath = "etc/accountcargo.etc"

type VaultTier struct {
	Capacity  int   `json:"capacity"`
	GoldLimit int64 `json:"goldLimit,string"`
}
type VaultDefinition struct {
	Revision string      `json:"revision"`
	Tiers    []VaultTier `json:"tiers"`
}

// A small indexed PVF document, never an archive scan. The six-column layout
// is capacity, gold limit, material ID/count, gold cost and shop commodity ID.
func ParseVaultDefinition(text, archiveSHA256 string) (VaultDefinition, error) {
	var result VaultDefinition
	if len(text) == 0 || len(text) > 64<<10 {
		return result, ErrVaultDefinition
	}
	doc, e := pvf.Parse(VaultDefinitionPath, text)
	if e != nil {
		return result, ErrVaultDefinition
	}
	sections, closed := 0, 0
	upgradeAt, closeAt := -1, -1
	for i, s := range doc.Sections {
		if s.Name == "upgrade info" {
			sections++
			upgradeAt = i
		}
		if s.Name == "/upgrade info" {
			closed++
			closeAt = i
		}
	}
	if sections != 1 || closed != 1 || closeAt != upgradeAt+1 {
		return result, ErrVaultDefinition
	}
	tokens, ok := doc.Section("upgrade info")
	if !ok || len(tokens) == 0 || len(tokens)%6 != 0 || len(tokens) > 256*6 {
		return result, ErrVaultDefinition
	}
	for _, token := range tokens {
		if token.Kind != pvf.TokenInt {
			return result, ErrVaultDefinition
		}
	}
	previous := int64(0)
	for i := 0; i < len(tokens); i += 6 {
		capacity, gold := tokens[i].Int, tokens[i+1].Int
		if capacity <= previous || capacity > math.MaxInt16 || gold < 0 || tokens[i+2].Int < -1 || tokens[i+3].Int < 0 || tokens[i+4].Int < 0 || tokens[i+5].Int < -1 {
			return result, ErrVaultDefinition
		}
		if gold > MaxVaultGold {
			gold = MaxVaultGold
		}
		result.Tiers = append(result.Tiers, VaultTier{Capacity: int(capacity), GoldLimit: gold})
		previous = capacity
	}
	sum := sha256.Sum256([]byte("account-vault-definition-v1\x00" + archiveSHA256 + "\x00" + text))
	result.Revision = hex.EncodeToString(sum[:])
	return result, nil
}
func (d VaultDefinition) GoldLimit(capacity int) (int64, bool) {
	for _, tier := range d.Tiers {
		if tier.Capacity == capacity {
			return tier.GoldLimit, true
		}
	}
	return 0, false
}
