//go:build acceptance

// Package setupjava_test exercises setup-java against real infrastructure:
// real JDK downloads and, when running inside a job, a real CircleCI
// task-agent cache. Tagged out of `task test` because it needs the network.
// The CLI contract itself is covered hermetically by clitest/.
package setupjava_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"
)

// binaryPathEnvVar names a prebuilt setup-java to test, for an executor with
// no Go toolchain to compile one with. The smoke jobs set it, and pair it with
// a cross-compiled copy of this suite. A relative path is resolved here, since
// the suite runs the binary from a working directory of its own.
const binaryPathEnvVar = "SETUP_JAVA_BINARY"

var binaryPath string

func TestMain(m *testing.M) {
	status, err := runTests(m)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	os.Exit(status)
}

func runTests(m *testing.M) (int, error) {
	if bin := os.Getenv(binaryPathEnvVar); bin != "" {
		abs, err := filepath.Abs(bin)
		if err != nil {
			return 0, fmt.Errorf("%s=%q: %w", binaryPathEnvVar, bin, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return 0, fmt.Errorf("%s=%q: %w", binaryPathEnvVar, bin, err)
		}
		binaryPath = abs
		return m.Run(), nil
	}

	p := compiler.NewParallel(1)
	defer p.Cleanup()

	p.Add(compiler.Work{
		Result: &binaryPath,
		Name:   "setup-java",
		Target: "..",
		Source: ".",
	})

	if err := p.Run(context.Background()); err != nil {
		return 0, err
	}
	return m.Run(), nil
}

// runFunc runs the compiled binary with extraEnv applied on top of the
// sandbox's environment, so entries in extraEnv take precedence.
type runFunc func(t *testing.T, extraEnv map[string]string, args ...string) (string, error)

// sandbox returns a sandbox plus a run function bound to its env.
//
// The cache is CircleCI's own when the caller asks for it with
// TOOLKIT_CACHE_MODE=live, which the CI job does, and in-memory otherwise, so
// the same suite runs on a laptop. Either way $HOME and $BASH_ENV are temp
// locations, so an install never touches the developer's real ones.
func sandbox(t *testing.T) (*sdktest.Sandbox, runFunc) {
	t.Helper()
	s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
	return s, func(t *testing.T, extraEnv map[string]string, args ...string) (string, error) {
		t.Helper()
		env := s.Env()
		maps.Copy(env, extraEnv)
		r := runner.New(t, binaryPath, runner.WithSandboxEnv(env))
		_, err := r.Run(args...)
		return r.StdoutString() + r.StderrString(), err
	}
}

// acceptanceVersion returns the Java version spec to install. CI sets
// SETUP_JAVA_ACCEPTANCE_VERSION from the pipeline's java-version parameter,
// so every job in a pipeline installs the same release.
func acceptanceVersion() string {
	if v := os.Getenv("SETUP_JAVA_ACCEPTANCE_VERSION"); v != "" {
		return v
	}
	return "21"
}
