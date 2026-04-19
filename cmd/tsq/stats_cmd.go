package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Gjdoalfnrxu/tsq/extract/db"
	"github.com/Gjdoalfnrxu/tsq/ql/stats"
)

// cmdStats dispatches `tsq stats <subcmd>` for sidecar diagnostics.
//
//	tsq stats compute <db>     # rebuild sidecar for an existing EDB
//	tsq stats inspect <db> [rel]
func cmdStats(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: tsq stats <compute|inspect> <db> [args]")
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "compute":
		return cmdStatsCompute(ctx, rest, stdout, stderr)
	case "inspect":
		return cmdStatsInspect(ctx, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "error: unknown stats subcommand %q\n", sub)
		fmt.Fprintln(stderr, "subcommands: compute, inspect")
		return 2
	}
}

func cmdStatsCompute(_ context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stats compute", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	pos := fs.Args()
	if len(pos) != 1 {
		fmt.Fprintln(stderr, "usage: tsq stats compute <db>")
		return 2
	}
	edbPath := pos[0]
	database, err := loadDB(edbPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: load db: %v\n", err)
		return 1
	}
	hash, err := stats.HashFile(edbPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: hash: %v\n", err)
		return 1
	}
	s, err := stats.Compute(database, hash)
	if err != nil {
		fmt.Fprintf(stderr, "error: compute: %v\n", err)
		return 1
	}
	if err := stats.Save(edbPath, s); err != nil {
		fmt.Fprintf(stderr, "error: save: %v\n", err)
		return 1
	}
	if info, err := os.Stat(stats.SidecarPath(edbPath)); err == nil {
		fmt.Fprintf(stdout, "wrote %s (%d bytes)\n", stats.SidecarPath(edbPath), info.Size())
	}
	return 0
}

func cmdStatsInspect(_ context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("stats inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	pos := fs.Args()
	if len(pos) < 1 || len(pos) > 2 {
		fmt.Fprintln(stderr, "usage: tsq stats inspect <db> [rel]")
		return 2
	}
	edbPath := pos[0]
	relFilter := ""
	if len(pos) == 2 {
		relFilter = pos[1]
	}
	s, err := stats.Load(edbPath, stderr)
	if err != nil {
		// Load already warned; exit non-zero so scripts notice.
		return 1
	}
	stats.Inspect(stdout, s, relFilter)
	return 0
}

// loadDB reads an EDB from disk.
func loadDB(path string) (*db.DB, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return db.ReadDB(f, info.Size())
}
