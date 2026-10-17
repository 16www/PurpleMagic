// Package expertjob provides compact, PVF-backed management definitions.
package expertjob

import (
	"errors"
	"math"
	"sort"
	"strings"

	"dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/quest"
)

var ErrDefinition = errors.New("invalid expert job definition")
var ErrInput = errors.New("invalid expert job target")

var Paths = []string{"character/expertjob/enchanter.exj", "character/expertjob/alchemist.exj", "character/expertjob/disjointer.exj", "character/expertjob/doll_controller.exj"}

type Definition struct {
	Type             int           `json:"type"`
	Name             string        `json:"name"`
	Thresholds       []int64       `json:"thresholds"`
	AutoRecipes      map[int]int64 `json:"autoRecipes"`
	InitialEndurance int64         `json:"initialEndurance"`
	MinimumLevel     int           `json:"minimumLevel"`
	QuestIDs         []int64       `json:"questIds"`
}

// Level follows the game Config.Level / DisjointerConfig.Level algorithm.
// The final threshold is a terminal sentinel, not an additional level.
func (d Definition) Level(exp int64) int {
	if len(d.Thresholds) == 0 {
		return 0
	}
	level := 1
	for _, n := range d.Thresholds {
		if exp < n {
			break
		}
		level++
	}
	return min(level, len(d.Thresholds))
}
func (d Definition) Experience(level int) (int64, error) {
	if level < 1 || level > len(d.Thresholds) {
		return 0, ErrInput
	}
	if level == 1 {
		return 0, nil
	}
	return d.Thresholds[level-2], nil
}
func (d Definition) Validate() error {
	if d.Type < 1 || d.Type > 4 || d.Name == "" || len(d.Thresholds) < 1 || len(d.Thresholds) > 64 || d.MinimumLevel < 1 || d.MinimumLevel > 90 || len(d.QuestIDs) == 0 || len(d.QuestIDs) > 256 || len(d.AutoRecipes) > 255 || d.InitialEndurance < 0 || d.InitialEndurance > math.MaxInt32 {
		return ErrDefinition
	}
	last := int64(0)
	for _, n := range d.Thresholds {
		if n <= last || n > math.MaxUint32 {
			return ErrDefinition
		}
		last = n
	}
	last = 0
	for _, n := range d.QuestIDs {
		if n <= last {
			return ErrDefinition
		}
		last = n
	}
	for l, id := range d.AutoRecipes {
		if l < 1 || l > len(d.Thresholds) || id <= 0 || id > math.MaxUint32 {
			return ErrDefinition
		}
	}
	return nil
}

// ParseManagement reads just the four small profession documents and transition
// quest definitions. It never opens item, recipe, equipment or skill catalogs.
func ParseManagement(typ int, text string, quests []quest.Definition) (Definition, error) {
	d := Definition{Type: typ, AutoRecipes: map[int]int64{}, QuestIDs: []int64{}}
	if typ < 1 || typ > 4 {
		return d, ErrDefinition
	}
	d.Name = []string{"附魔师", "炼金术师", "分解师", "控偶师"}[typ-1]
	doc, e := pvf.Parse(Paths[typ-1], text)
	if e != nil {
		return d, e
	}
	tokens, ok := doc.Section("expertness exp")
	if !ok || len(tokens) == 0 || len(tokens)%3 != 0 {
		return d, ErrDefinition
	}
	for i := 0; i < len(tokens); i += 3 {
		if tokens[i].Kind != pvf.TokenInt {
			return d, ErrDefinition
		}
		d.Thresholds = append(d.Thresholds, tokens[i].Int)
	}
	vals := doc.Ints("auto learn recipe")
	if len(vals)%2 != 0 {
		return d, ErrDefinition
	}
	for i := 0; i < len(vals); i += 2 {
		if _, ok := d.AutoRecipes[int(vals[i])]; ok {
			return d, ErrDefinition
		}
		d.AutoRecipes[int(vals[i])] = vals[i+1]
	}
	if typ == 1 || typ == 3 {
		d.InitialEndurance, _ = doc.Int("endurance initial value")
		if d.InitialEndurance <= 0 {
			return d, ErrDefinition
		}
	}
	members := map[int64]quest.Definition{}
	chosen := map[int64]bool{}
	for _, q := range quests {
		if q.JobChangeQuest != 20 {
			return d, ErrDefinition
		}
		members[q.ID] = q
		if strings.Trim(strings.TrimSpace(q.RewardType), "[]") == "expert job" && len(q.RewardIntData) == 1 && q.RewardIntData[0] == int64(typ) {
			chosen[q.ID] = true
		}
	}
	// Follow only prerequisites in the explicit expert-job chain. All alternatives
	// are included, matching the management completion convention.
	for changed := true; changed; {
		changed = false
		for id := range chosen {
			q := members[id]
			for _, group := range q.PreRequiredGroups {
				for _, n := range group {
					if _, ok := members[n]; ok && !chosen[n] {
						chosen[n] = true
						changed = true
					}
				}
			}
		}
	}
	for id := range chosen {
		d.QuestIDs = append(d.QuestIDs, id)
		d.MinimumLevel = max(d.MinimumLevel, members[id].LevelMin)
	}
	sort.Slice(d.QuestIDs, func(i, j int) bool { return d.QuestIDs[i] < d.QuestIDs[j] })
	return d, d.Validate()
}
