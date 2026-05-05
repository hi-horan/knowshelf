package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"knowshelf/internal/config"
	auth "knowshelf/internal/pkg/auth"

	"github.com/spf13/cobra"
)

type tokenGenOptions struct {
	subject string
	scopes  []string
	ttl     time.Duration
}

func newTokenGenCommand(configPath *string) *cobra.Command {
	opts := tokenGenOptions{}
	cmd := &cobra.Command{
		Use:   "token_gen",
		Short: "Generate a signed MCP bearer token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return generateTokenFromConfig(*configPath, opts, time.Now(), os.Stdout)
		},
	}
	cmd.Flags().StringVar(&opts.subject, "sub", "", "token subject")
	cmd.Flags().StringArrayVar(&opts.scopes, "scope", nil, "token scope; repeat or use comma-separated values")
	cmd.Flags().DurationVar(&opts.ttl, "ttl", 0, "token time to live, e.g. 24h")
	return cmd
}

func generateTokenFromConfig(configPath string, opts tokenGenOptions, now time.Time, out io.Writer) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	claims, err := auth.NewTokenClaims(opts.subject, opts.scopes, now, opts.ttl)
	if err != nil {
		return err
	}
	token, err := auth.GenerateToken(cfg.MCP.Auth.Secret, claims)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, token)
	return err
}
