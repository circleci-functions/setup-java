//go:build acceptance

package setupjava_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/circleci-functions/toolkit/sdk/sdktest"
	"github.com/circleci-functions/toolkit/testing/runner"
)

func TestAcceptance_CacheDependencies(t *testing.T) {
	for _, tool := range []struct{ name, file, label string }{
		{name: "maven", file: "pom.xml", label: "Maven dependencies"},
		{name: "gradle", file: "build.gradle", label: "Gradle dependencies"},
	} {
		t.Run(tool.name, func(t *testing.T) {
			s := sdktest.NewSandbox(t, sdktest.WithLiveCache())
			dir := t.TempDir()
			assert.NilError(t, os.WriteFile(filepath.Join(dir, tool.file), []byte("# "+s.CacheKeyNonce), 0600))

			r := runner.New(t, binaryPath, runner.WithSandboxEnv(s.Env()), runner.WithCmdDir(dir))
			_, err := r.Run("cache-dependencies", "--build-tool", tool.name, "--cache-nonce", s.CacheKeyNonce)
			out := r.StdoutString() + r.StderrString()
			assert.NilError(t, err, "output: %s", out)
			assert.Assert(t, strings.Contains(out, "restored "+tool.label+" if cached under setup-java-v1-"+tool.name+"-"), "output: %s", out)
			assert.Assert(t, strings.Contains(out, "saves it there when the job succeeds"), "output: %s", out)
		})
	}
}
