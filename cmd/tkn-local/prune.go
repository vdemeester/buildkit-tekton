package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/util/appcontext"
	"github.com/spf13/cobra"
	"github.com/tonistiigi/units"
	"github.com/vdemeester/buildkit-tekton/pkg/buildkit"
)

type pruneOption struct {
	filters  []string
	host     string
	duration time.Duration
	storage  float64
	all      bool
	verbose  bool
}

func pruneCommand() *cobra.Command {
	opts := &pruneOption{}
	cmd := &cobra.Command{
		Use:     "prune",
		Aliases: []string{},
		Short:   "clean up buildkit build cache",
		RunE: func(cmd *cobra.Command, args []string) error {
			return prune(opts)
		},
	}
	cmd.Flags().StringVar(&opts.host, "host", "", "Host to use")
	cmd.Flags().StringArrayVar(&opts.filters, "filter", []string{}, "Filter records")
	cmd.Flags().DurationVar(&opts.duration, "keep-duration", 0, "keep data newer than this limit")
	cmd.Flags().Float64Var(&opts.storage, "keep-storage", 0, "keep data below this limit (in MB)")
	cmd.Flags().BoolVar(&opts.all, "all", false, "include internal/frontend references")
	cmd.Flags().BoolVarP(&opts.verbose, "verbose", "v", false, "verbose output")

	return cmd
}

func prune(opts *pruneOption) error {
	ctx := appcontext.Context()

	// Connect or start buildkit
	c, err := buildkit.NewClient(ctx, opts.host)
	if err != nil {
		return err
	}

	ch := make(chan client.UsageInfo)
	printed := make(chan struct{})

	tw := tabwriter.NewWriter(os.Stdout, 1, 8, 1, '\t', 0)
	first := true
	total := int64(0)

	go func() {
		defer close(printed)
		for du := range ch {
			total += du.Size
			if opts.verbose {
				printVerbose(tw, []*client.UsageInfo{&du})
			} else {
				if first {
					printTableHeader(tw)
					first = false
				}
				printTableRow(tw, &du)
				tw.Flush()
			}
		}
	}()

	clientopts := []client.PruneOption{
		client.WithFilter(opts.filters),
		client.WithKeepOpt(opts.duration, int64(opts.storage*1e6)),
	}

	if opts.all {
		clientopts = append(clientopts, client.PruneAll)
	}

	err = c.Prune(ctx, ch, clientopts...)
	close(ch)
	if err != nil {
		return err
	}

	return nil
}

func printVerbose(tw *tabwriter.Writer, du []*client.UsageInfo) {
	for _, di := range du {
		printKV(tw, "ID", di.ID)
		if len(di.Parents) > 0 {
			printKV(tw, "Parents", strings.Join(di.Parents, ";"))
		}
		printKV(tw, "Created at", di.CreatedAt)
		printKV(tw, "Mutable", di.Mutable)
		printKV(tw, "Reclaimable", !di.InUse)
		printKV(tw, "Shared", di.Shared)
		printKV(tw, "Size", fmt.Sprintf("%.2f", units.Bytes(di.Size)))
		if di.Description != "" {
			printKV(tw, "Description", di.Description)
		}
		printKV(tw, "Usage count", di.UsageCount)
		if di.LastUsedAt != nil {
			printKV(tw, "Last used", di.LastUsedAt)
		}
		if di.RecordType != "" {
			printKV(tw, "Type", di.RecordType)
		}

		fmt.Fprintf(tw, "\n")
	}

	tw.Flush()
}

func printTableHeader(tw *tabwriter.Writer) {
	fmt.Fprintln(tw, "ID\tRECLAIMABLE\tSIZE\tLAST ACCESSED")
}

func printTableRow(tw *tabwriter.Writer, di *client.UsageInfo) {
	id := di.ID
	if di.Mutable {
		id += "*"
	}
	size := fmt.Sprintf("%.2f", units.Bytes(di.Size))
	if di.Shared {
		size += "*"
	}
	fmt.Fprintf(tw, "%-71s\t%-11v\t%s\t\n", id, !di.InUse, size)
}

func printKV(w io.Writer, k string, v interface{}) {
	fmt.Fprintf(w, "%s:\t%v\n", k, v)
}
