package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/turbokube/contain/pkg/ocipush"
	"go.uber.org/zap"
)

var (
	proxyAddr     string
	proxyUpstream string
	proxyPrefix   string
	proxyOptions  ocipush.Options
)

func newRegistryProxyCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "registry-proxy --upstream <registry-host>",
		Short: "Local registry endpoint that forwards to an upstream registry",
		Long: `Serves an unauthenticated OCI registry endpoint on localhost that
forwards to an upstream registry, so stock docker/buildkit tooling can push
any layer size to registries whose proxied upload path is size-limited
(e.g. behind Cloudflare's request body cap):

  contain registry-proxy --upstream registry.example.com &
  docker push localhost:5000/app/name:tag

Blob uploads are staged locally, digest-verified, and re-uploaded — large
blobs via the upstream's direct-to-storage extension (presigned URLs).
Pulls pass through. Upstream credentials resolve like docker/crane
(docker login), so run docker login for the upstream host first.

Only bind to localhost or otherwise trusted networks: the proxy itself
does not authenticate clients.`,
		Args: cobra.NoArgs,
		RunE: runRegistryProxy,
	}
	c.Flags().StringVar(&proxyAddr, "addr", "127.0.0.1:5000", "listen address")
	c.Flags().StringVar(&proxyUpstream, "upstream", "", "upstream registry host (required)")
	c.Flags().StringVar(&proxyPrefix, "prefix", "", "repository name prefix to add upstream, must end with /")
	addDirectUploadFlags(c, &proxyOptions, "upstream")
	cobra.CheckErr(c.MarkFlagRequired("upstream"))
	return c
}

func runRegistryProxy(cmd *cobra.Command, args []string) error {
	logger := newLogger()
	defer logger.Sync() //nolint:errcheck
	undo := zap.ReplaceGlobals(logger)
	defer undo()

	proxy, err := ocipush.NewProxy(proxyUpstream, proxyPrefix, proxyOptions)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Addr: proxyAddr, Handler: proxy}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		server.Shutdown(shutdownCtx) //nolint:errcheck
	}()

	zap.L().Info("registry-proxy listening",
		zap.String("addr", proxyAddr), zap.String("upstream", proxyUpstream))
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
