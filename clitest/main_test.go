// Package clitest_test exercises the setup-java CLI contract end to end
// against fakes: a fake Adoptium API serving JDKs whose tools are shell
// scripts, and a fake CircleCI plugin server (sdktest's sandbox). It is
// hermetic: no network, and nothing outside temp directories is touched.
// Real installs and real-infra cache round-trips are covered by acceptance/.
package clitest_test

import (
	"context"
	"fmt"
	"maps"
	"os"
	"testing"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/compiler"
	"github.com/circleci-functions/toolkit/testing/runner"
)

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

// sandbox returns a sandbox plus a run function bound to its env. It is the
// only way to run the binary in this package, so no test can install into the
// developer's real home directory or reach a cache it did not set up.
func sandbox(t *testing.T) (*sdktest.Sandbox, runFunc) {
	t.Helper()
	s := sdktest.NewSandbox(t)
	return s, func(t *testing.T, extraEnv map[string]string, args ...string) (string, error) {
		t.Helper()
		env := s.Env()
		maps.Copy(env, extraEnv)
		r := runner.New(t, binaryPath, runner.WithSandboxEnv(env))
		_, err := r.Run(args...)
		return r.StdoutString() + r.StderrString(), err
	}
}
