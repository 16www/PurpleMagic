package gmtool

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	platformpvf "dnf90.local/gateway/internal/platform/pvf"
)

func TestV1ExternalApprovedPVF(t *testing.T) {
	path := os.Getenv("DNF90GM_EXTERNAL_PVF")
	if path == "" {
		t.Skip("approved PVF fixture not configured")
	}
	ctx := context.Background()
	source, e := platformpvf.OpenDisk(path, os.Getenv("DNF90GM_EXTERNAL_DIRECTORY"), os.Getenv("DNF90GM_EXTERNAL_HASH"))
	if e != nil {
		t.Fatal(e)
	}
	defer source.Close()
	original, e := NewCatalog(ctx, Config{Password: strings.Repeat("p", 48), ItemIndexCachePath: os.Getenv("DNF90GM_EXTERNAL_ITEMS")}, source)
	if e != nil {
		t.Fatal(e)
	}
	first := original.items.snapshot()[0].ID
	firstQuest := original.questIndex.Rows[0].ID
	local, _, _ := externalTestServices(t, original)
	item, ok, e := original.deps.Catalog.item(first)
	want, wok := local.items.lookup(first)
	if e != nil || !ok || !wok || !reflect.DeepEqual(item, want) {
		t.Fatal("item definition", e)
	}
	if e = original.deps.Catalog.preload(ctx, []uint32{first, 3228, 400330105}); e != nil {
		t.Fatal(e)
	}
	a, e := local.vaultDefinition()
	if e != nil {
		t.Fatal(e)
	}
	b, e := original.vaultDefinition()
	if e != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("vault", e)
	}
	p, e := local.skillPointDefinition(ctx, 90)
	if e != nil {
		t.Fatal(e)
	}
	q, e := original.skillPointDefinition(ctx, 90)
	if e != nil || !reflect.DeepEqual(p, q) {
		t.Fatal("skill points", e)
	}
	x, e := local.expertJobDefinitions(ctx)
	if e != nil {
		t.Fatal(e)
	}
	y, e := original.expertJobDefinitions(ctx)
	if e != nil || !reflect.DeepEqual(x, y) {
		t.Fatal("expert jobs", e)
	}
	for _, job := range []byte{0, 2} {
		ix, res, quests, e := local.professionManagementResources(ctx, job)
		if e != nil {
			t.Fatal(e)
		}
		rx, rr, rq, e := original.professionManagementResources(ctx, job)
		if e != nil || !reflect.DeepEqual(ix.Manifest, rx.Manifest) || !reflect.DeepEqual(res.Profiles.Snapshot(), rr.Profiles.Snapshot()) || !reflect.DeepEqual(res.Skills.Snapshot(), rr.Skills.Snapshot()) || !reflect.DeepEqual(quests.ProfessionDefinitionIDs(), rq.ProfessionDefinitionIDs()) {
			t.Fatal("profession", job, e)
		}
	}
	c := repository.CharacterRecord{Job: "0", Stats: map[string]int64{"grow_type": 1}}
	l, e := local.levelDefinition(ctx, c, 90)
	if e != nil {
		t.Fatal(e)
	}
	m, e := original.levelDefinition(ctx, c, 90)
	if e != nil || !reflect.DeepEqual(l, m) {
		t.Fatal("growth", e)
	}
	ix, e := original.questReadIndex(ctx)
	if e != nil {
		t.Fatal(e)
	}
	d, e := ix.definition(ctx, firstQuest)
	if e != nil {
		t.Fatal(e)
	}
	di, _ := local.questReadIndex(ctx)
	expected, e := di.definition(ctx, firstQuest)
	if e != nil || !reflect.DeepEqual(d, expected) {
		t.Fatal("quest", e)
	}
	if len(original.items.entries) != 0 || len(ix.Rows) != 0 || original.db != nil {
		t.Fatal("unexpected local catalog or database")
	}
}
