// Command setup-java installs a Java Development Kit and makes it the one later
// steps of a CircleCI job run.
//
// Supported platforms: linux (glibc and musl), darwin, and windows.
package main

import (
	"context"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"
)

type cli struct {
	Install installCmd `cmd:"" default:"withargs" help:"Install a JDK, put java, javac and the other JDK tools on PATH, and set JAVA_HOME. Runs when no command is given."`
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-java"),
		kong.Description("Install a Java Development Kit, caching the download."),
		kong.UsageOnError(),
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run(ctx)
}
