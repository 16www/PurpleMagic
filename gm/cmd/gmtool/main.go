package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Cec1c/DNF90GM/internal/app/gmadmin"
	platformpvf "github.com/Cec1c/DNF90GM/internal/platform/pvf"
	"github.com/Cec1c/DNF90GM/internal/services/gmtool"
)

func main() {
	path := flag.String("config", "", "protected GM JSON config")
	initSchema := flag.Bool("init-schema", false, "initialize additive GM benefit tables")
	check := flag.Bool("check", false, "validate configuration without connecting or starting services")
	buildPVF := flag.Bool("build-pvf-index", false, "build a new disk PVF index offline and exit; no database or listener")
	buildDetails := flag.Bool("build-item-details", false, "compile or reuse the PVF item detail index offline and exit")
	buildQuests := flag.Bool("build-quest-index", false, "compile or reuse the PVF quest index offline and exit")
	flag.Parse()
	operations := 0
	for _, enabled := range []bool{*check, *buildPVF, *initSchema, *buildDetails, *buildQuests} {
		if enabled {
			operations++
		}
	}
	if operations > 1 {
		slog.Error("--check, --build-pvf-index, --build-item-details, --build-quest-index and --init-schema are separate operations")
		os.Exit(1)
	}
	cfg, err := gmadmin.ReadConfig(*path)
	if err != nil {
		slog.Error("GM configuration rejected", "error", err)
		os.Exit(1)
	}
	if *check {
		slog.Info("GM configuration valid")
		return
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *buildPVF {
		if err = platformpvf.BuildDiskIndex(ctx, cfg.PVFPath, cfg.PVFSHA256, cfg.DiskIndexPath()); err != nil {
			slog.Error("PVF disk index build failed", "error", err)
			os.Exit(1)
		}
		slog.Info("PVF disk index built; offline importer exiting")
		return
	}
	if *buildDetails || *buildQuests {
		archive, e := platformpvf.OpenDisk(cfg.PVFPath, cfg.DiskIndexPath(), cfg.PVFSHA256)
		if e != nil {
			slog.Error("PVF disk index unavailable", "error", e)
			os.Exit(1)
		}
		if *buildQuests {
			report, questErr := gmtool.BuildQuestIndex(ctx, archive, cfg.ItemIndexPath)
			archive.Close()
			if questErr != nil {
				slog.Error("quest index failed", "error", questErr)
				os.Exit(1)
			}
			slog.Info("quest index ready", "path", report.Path, "quests", report.Quests, "reused", report.Reused)
			return
		}
		report, e := gmtool.BuildItemDetails(ctx, archive, cfg.ItemIndexPath, slog.Info)
		archive.Close()
		if e != nil {
			slog.Error("item detail index failed", "error", e)
			os.Exit(1)
		}
		slog.Info("item detail index ready", "path", report.Path, "items", report.Items, "failed", report.Failed, "bytes", report.Bytes, "reused", report.Reused)
		return
	}
	if err = gmadmin.Run(ctx, cfg, *initSchema, slog.Info); err != nil {
		slog.Error("GM stopped", "error", err)
		os.Exit(1)
	}
}
