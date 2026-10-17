package pvf

import (
	"context"
	"testing"

	platformpvf "github.com/Cec1c/DNF90GM/internal/platform/pvf"
)

type resolvedTestSource struct{ *memSource }

func (s resolvedTestSource) Files() []platformpvf.File { panic("full directory enumeration") }
func (s resolvedTestSource) ResolvePath(value string) (string, bool) {
	for name := range s.texts {
		if pathKey(name) == pathKey(value) {
			return name, true
		}
	}
	return "", false
}

func TestListBuildUsesResolverWithoutMaterializingDirectory(t *testing.T) {
	source := resolvedTestSource{&memSource{texts: map[string]string{"skill/skilllist.lst": "0 `Nested.lst`", "skill/Nested.lst": "12 `skill/Jobs/A.skl`", "skill/Jobs/A.skl": "[name]\n`fixture`"}, reads: make(map[string]int)}}
	index, err := Build(context.Background(), source, BuildOptions{Lists: []string{"SKILL/SKILLLIST.LST"}})
	if err != nil {
		t.Fatal(err)
	}
	if path, ok := index.Resolve("skill/Nested.lst", 12); !ok || path != "skill/Jobs/A.skl" {
		t.Fatal(path, ok)
	}
	if index.Snapshot().Documents != 3 {
		t.Fatal(index.Snapshot())
	}
}
