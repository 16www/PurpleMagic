// gmindex prepares GM static definitions offline, without a database or listener.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	platformpvf "dnf90.local/gateway/internal/platform/pvf"
	"dnf90.local/gateway/internal/services/gmtool"
)

func main() {
	file := flag.String("pvf", "", "approved Script.pvf path")
	hash := flag.String("sha256", "", "full approved PVF SHA256")
	directory := flag.String("directory-index", "", "existing PVF disk directory")
	summary := flag.String("item-index", "", "item summary path beside the output indexes")
	flag.Parse()
	if flag.NArg() != 0 || *file == "" || *hash == "" || *directory == "" || *summary == "" {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if e := run(ctx, *file, *directory, *hash, *summary); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(ctx context.Context, file, directory, hash, summary string) error {
	source, e := platformpvf.OpenDisk(file, directory, hash)
	if e != nil {
		return e
	}
	defer source.Close()
	report, e := gmtool.BuildProfessionIndex(ctx, source, summary)
	if e != nil {
		return e
	}
	expert, e := gmtool.BuildExpertJobIndex(ctx, source, summary)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"profession": report, "expertJob": expert})
}
