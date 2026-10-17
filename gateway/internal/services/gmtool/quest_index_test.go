package gmtool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
)

func questFixture(name string) *itemCacheTestSource {
	return newItemCacheTestSource(readonlyTestSource{
		"n_quest/quest.lst": "1 `one.qst`\n2 `two.qst`\n",
		"n_quest/one.qst":   "#PVF_File\n[name]\n`" + name + "`\n[grade]\n`[epic]`\n[level]\n1 90\n[condition message]\n`收集材料 <script>`\n[pre required quest]\n2\n[type]\n`[hunt monster]`\n[check count]\n3\n",
		"n_quest/two.qst":   "#PVF_File\n[name]\n`第二个任务`\n[grade]\n`[normal]`\n[level]\n1 90\n",
	})
}

func TestV1QuestIndexGenerationChangeCorruptionAndCancellation(t *testing.T) {
	ctx := context.Background()
	summary := filepath.Join(t.TempDir(), "items.json")
	source := questFixture("原任务")
	report, err := BuildQuestIndex(ctx, source, summary)
	if err != nil {
		t.Fatal(err)
	}
	if report.Quests != 2 || report.Reused {
		t.Fatal(report)
	}
	ix, err := readQuestIndex(ctx, report.Path, source.checksum)
	if err != nil {
		t.Fatal(err)
	}
	d, err := ix.definition(ctx, 1)
	if err != nil || d.Name != "原任务" || !strings.Contains(d.ConditionMessage, "<script>") {
		t.Fatal(d, err)
	}
	before := source.reads
	report, err = BuildQuestIndex(ctx, source, summary)
	if err != nil || !report.Reused || source.reads != before {
		t.Fatal("warm index read PVF", report, err)
	}
	changed := questFixture("新任务")
	next, err := BuildQuestIndex(ctx, changed, summary)
	if err != nil || next.Path == report.Path {
		t.Fatal(next, err)
	}
	if _, err = readQuestIndex(ctx, report.Path, changed.checksum); err == nil {
		t.Fatal("old index accepted under new PVF")
	}
	s := &Server{items: &itemIndex{cachePath: summary}, deps: Deps{PVF: func() (dnfpvf.Source, error) { return source, nil }}}
	if _, err = s.questReadIndex(ctx); err != nil {
		t.Fatal(err)
	}
	s.deps.PVF = func() (dnfpvf.Source, error) { return changed, nil }
	active, err := s.questReadIndex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	d, err = active.definition(ctx, 1)
	if err != nil || d.Name != "新任务" {
		t.Fatal(d, err)
	}
	data, err := os.ReadFile(next.Path)
	if err != nil {
		t.Fatal(err)
	}
	data[questIndexHeader+3] ^= 0xff
	if err = os.WriteFile(next.Path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = active.definition(ctx, 1); err == nil {
		t.Fatal("corrupt quest served")
	}
	rebuilt, err := BuildQuestIndex(ctx, changed, summary)
	if err != nil || rebuilt.Reused {
		t.Fatal("corrupt generation not rebuilt", rebuilt, err)
	}
	current, _ := os.ReadFile(next.Path)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = BuildQuestIndex(canceled, changed, summary); err == nil {
		t.Fatal("cancellation ignored")
	}
	after, _ := os.ReadFile(next.Path)
	if string(current) != string(after) {
		t.Fatal("canceled build replaced valid index")
	}
	// Parser-version changes reject an otherwise intact generation.
	after[40]++
	if err = os.WriteFile(next.Path, after, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readQuestIndex(ctx, next.Path, changed.checksum); err == nil {
		t.Fatal("parser version ignored")
	}
}

func TestV1QuestIndexAddedRemovedAndFailedGeneration(t *testing.T) {
	ctx := context.Background()
	summary := filepath.Join(t.TempDir(), "items.json")
	old := questFixture("旧任务")
	previous, err := BuildQuestIndex(ctx, old, summary)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(previous.Path)
	if err != nil {
		t.Fatal(err)
	}
	updated := newItemCacheTestSource(readonlyTestSource{
		"n_quest/quest.lst": "2 `two.qst`\n3 `three.qst`\n",
		"n_quest/two.qst":   "#PVF_File\n[name]\n`保留任务`\n",
		"n_quest/three.qst": "#PVF_File\n[name]\n`新增任务`\n",
	})
	next, err := BuildQuestIndex(ctx, updated, summary)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := readQuestIndex(ctx, next.Path, updated.checksum)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ix.definition(ctx, 1); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("removed definition survived generation change", err)
	}
	added, err := ix.definition(ctx, 3)
	if err != nil || added.Name != "新增任务" || next.Quests != 2 {
		t.Fatal("new definition unavailable", added, err)
	}
	broken := newItemCacheTestSource(readonlyTestSource{
		"n_quest/quest.lst": "1 `missing.qst`\n",
	})
	failed, err := BuildQuestIndex(ctx, broken, summary)
	if err == nil {
		t.Fatal("missing referenced document accepted")
	}
	if _, err = os.Stat(failed.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed generation was published", err)
	}
	after, err := os.ReadFile(previous.Path)
	if err != nil || string(before) != string(after) {
		t.Fatal("old generation changed", err)
	}
}
