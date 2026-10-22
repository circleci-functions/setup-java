// Command setup-java installs a Java Development Kit and makes it the one later
// steps of a CircleCI job run.
//
// Supported platforms: linux (glibc and musl), darwin, and windows.
package main

import (
	"context"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/circleci-functions/toolkit/functionyaml"
	"github.com/circleci-functions/toolkit/sdk"

	"github.com/circleci-functions/setup-java/depcache"
	"github.com/circleci-functions/setup-java/javadist"
)

type cli struct {
	Install           installCmd   `cmd:"" default:"withargs" help:"Install a JDK, put java, javac and the other JDK tools on PATH, and set JAVA_HOME. Runs when no command is given."`
	CacheDependencies cacheDepsCmd `cmd:"" name:"cache-dependencies" help:"Cache what a build tool downloads, without installing a JDK: restore it now, and save it at the end of the job if the job succeeds."`
}

func main() {
	sdk.Run(run)
}

func run(ctx context.Context, client *sdk.Client) error {
	var flags cli
	kCtx := kong.Parse(&flags,
		kong.Name("setup-java"),
		kong.Description("Install a Java Development Kit, and optionally cache what Maven or Gradle downloads."),
		kong.UsageOnError(),
		kong.Vars{
			"distributions":     strings.Join(javadist.Names(), ","),
			"distribution_list": strings.Join(javadist.Names(), ", "),
			"build_tools":       strings.Join(depcache.Names(), ","),
			"build_tool_list":   strings.Join(depcache.Names(), " or "),
		},
		functionyaml.Embed(),
	)
	kCtx.BindTo(ctx, (*context.Context)(nil))
	kCtx.Bind(client)
	return kCtx.Run(ctx)
}
