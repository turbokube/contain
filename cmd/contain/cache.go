package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	containcache "github.com/turbokube/contain/pkg/cache"
	"go.uber.org/zap"
)

func newCacheCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cache",
		Short: "Manage base image layer cache",
	}
	c.AddCommand(newCacheInfoCmd())
	c.AddCommand(newCachePurgeCmd())
	return c
}

func newCacheInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show cache location, entry count, and size",
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := containcache.New(zap.L())
			if err != nil {
				return err
			}
			count, bytes, err := lc.Info()
			if err != nil {
				return err
			}
			fmt.Printf("Path:    %s\n", lc.Dir())
			fmt.Printf("Entries: %d\n", count)
			fmt.Printf("Size:    %.1f MB\n", float64(bytes)/1024/1024)
			return nil
		},
	}
}

func newCachePurgeCmd() *cobra.Command {
	var (
		purgeAll     bool
		maxSizeMB    int64
		maxAgeDays   int
	)
	c := &cobra.Command{
		Use:   "purge",
		Short: "Remove cache entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := containcache.New(zap.L())
			if err != nil {
				return err
			}
			strategy := containcache.PurgeStrategy{
				All: purgeAll,
			}
			if maxSizeMB > 0 {
				strategy.MaxSize = maxSizeMB * 1024 * 1024
			}
			if maxAgeDays > 0 {
				strategy.MaxAge = time.Duration(maxAgeDays) * 24 * time.Hour
			}
			result, err := lc.Purge(strategy)
			if err != nil {
				return err
			}
			fmt.Printf("Removed: %d entries (%.1f MB)\n", result.RemovedCount, float64(result.RemovedBytes)/1024/1024)
			fmt.Printf("Kept:    %d entries (%.1f MB)\n", result.RetainedCount, float64(result.RetainedBytes)/1024/1024)
			return nil
		},
	}
	c.Flags().BoolVar(&purgeAll, "all", false, "remove all cached layers")
	c.Flags().Int64Var(&maxSizeMB, "max-size-mb", 0, "evict oldest entries until cache is at or below this size in MB")
	c.Flags().IntVar(&maxAgeDays, "max-age-days", 0, "remove entries not accessed in this many days")
	return c
}
