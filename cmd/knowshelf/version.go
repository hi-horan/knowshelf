package main

import (
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"

	"github.com/spf13/cobra"
)

const unknownBuildValue = "unknown"

var (
	gitVersion = unknownBuildValue
	buildTime  = unknownBuildValue
	gitTag     = unknownBuildValue
)

type versionInfo struct {
	GitVersion string
	BuildTime  string
	GitTag     string
}

func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print build version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return writeVersionInfo(cmd.OutOrStdout(), currentVersionInfo())
		},
	}
	return cmd
}

func currentVersionInfo() versionInfo {
	info := versionInfo{
		GitVersion: buildValue(gitVersion),
		BuildTime:  buildValue(buildTime),
		GitTag:     buildValue(gitTag),
	}
	if info.GitVersion != unknownBuildValue {
		return info
	}
	if revision, modified, ok := vcsRevision(); ok {
		info.GitVersion = revision
		if modified {
			info.GitVersion += "-dirty"
		}
	}
	return info
}

func writeVersionInfo(out io.Writer, info versionInfo) error {
	_, err := fmt.Fprintf(out, "git_version=%s\nbuild_time=%s\ntag=%s\n",
		info.GitVersion, info.BuildTime, info.GitTag)
	return err
}

func versionInfoAttrs(info versionInfo) []slog.Attr {
	return []slog.Attr{
		slog.String("git_version", info.GitVersion),
		slog.String("build_time", info.BuildTime),
		slog.String("tag", info.GitTag),
	}
}

func buildValue(value string) string {
	if value == "" {
		return unknownBuildValue
	}
	return value
}

func vcsRevision() (revision string, modified bool, ok bool) {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false, false
	}
	for _, setting := range buildInfo.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		default:
		}
	}
	return revision, modified, revision != ""
}
