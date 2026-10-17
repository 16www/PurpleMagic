package skill

import "fmt"

// TableFromDefinitions restores one immutable job catalog from a verified disk
// record. It does not retain parser documents or any other job's definitions.
func TableFromDefinitions(job byte, definitions []Skill) (*Table, error) {
	if len(definitions) == 0 || len(definitions) > 4096 {
		return nil, ErrListEmpty
	}
	t := &Table{byKey: map[Key]int{}, byPath: map[jobPathKey]int{}, byJob: map[byte][]int{}}
	for _, d := range definitions {
		if d.Job != job || pathKey(d.Path) == "" {
			return nil, ErrDocMissing
		}
		if _, ok := t.byKey[d.Key()]; ok {
			return nil, fmt.Errorf("duplicate skill %d", d.ID)
		}
		index := len(t.skills)
		t.skills = append(t.skills, cloneSkill(d))
		t.byKey[d.Key()] = index
		t.byJob[job] = append(t.byJob[job], index)
		key := jobPathKey{Job: job, Path: pathKey(d.Path)}
		if _, ok := t.byPath[key]; !ok {
			t.byPath[key] = index
		}
	}
	return t, nil
}
