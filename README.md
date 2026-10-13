# setup-java

Installs a Java Development Kit (JDK) and makes it the one the rest of the job
uses: `java`, `javac` and the other JDK tools go on `PATH`, and `JAVA_HOME`
points at it.

```yaml
functions:
  setup-java: github.com/circleci-functions/setup-java@<version>

jobs:
  build:
    docker:
      - image: cimg/base:current
    steps:
      - checkout
      - setup-java:
          with:
            distribution: temurin
            java-version: "21"
      - run: ./mvnw verify
```

Where the `functions:` block is not available, run it as a step instead:

```yaml
- run: circleci run github.com/circleci-functions/setup-java@<version> -- --distribution temurin --java-version 21
```

## Installing a JDK

setup-java looks up the newest build that matches the version you ask for,
downloads it, checks it against the vendor's SHA-256, and unpacks it under
`~/.cache/setup-java`. It then runs the new `java` once, so a JDK that cannot
run on the machine fails this step rather than a later one.

It links `java`, `javac` and every other tool in the JDK's `bin` directory into
a directory already on `PATH`, ahead of any JDK the image ships. Later steps
run them by name, in any shell, without sourcing anything.

### Choosing a distribution

`--distribution` is required. There is no default.

| Distribution | Installs |
|--------------|----------|
| `temurin` | [Eclipse Temurin](https://adoptium.net), from the Adoptium API |
| `jdkfile` | the JDK archive given by `--jdk-file` |

### Choosing a version

Pass `--java-version`, `--java-version-file`, or both. One is required.

`--java-version` takes any of:

| Spec | Installs |
|------|----------|
| `21`, `21.0`, `8` | the newest release of that version |
| `21.0.5`, `18.0.1.1` | the newest build of exactly that release |
| `21.0.5+11` | exactly that build |
| `1.8`, `1.8.0_292` | the same, in the old Java 8 notation |
| `>=17 <22`, `17.x` | the newest release in the range |
| `latest` | the newest release |
| `26-ea` | the newest early-access build of 26 |

`--java-version-file` reads the version from a file:

| File | Read from |
|------|-----------|
| `.java-version` | the first version in the file |
| `.tool-versions` | the `java` line, e.g. `java temurin-21.0.5+11.0.LTS` |
| `.sdkmanrc` | the `java` line, e.g. `java=21.0.5-tem` |

When the file names a distribution, as `temurin-` and `-tem` do above, it must
match `--distribution`. When you pass both flags, the version in the file must
match one of the `--java-version` values, or the step fails.

### Installing a local archive

```yaml
- setup-java:
    with:
      distribution: jdkfile
      jdk-file: ./vendor/jdk-21.0.5.tar.gz
      java-version: "21.0.5"
```

The archive can be a `.tar.gz`, `.tgz` or `.zip`, with the JDK at its top level
or in a single directory inside it. `--java-version` names the version it holds,
and setup-java checks the JDK reports the same major version.

### Several JDKs, and build-tool toolchains

Build tools can compile with one JDK and test with another. Maven and Gradle
call this a toolchain, and find the JDKs by version through variables like
`JAVA_HOME_17_X64`.

Separate versions with commas to install each. The last one becomes the
default:

```yaml
- setup-java:
    with:
      distribution: temurin
      java-version: "17,21"
```

Every JDK installed gets a `JAVA_HOME_<major>_<ARCH>` variable, e.g.
`JAVA_HOME_17_X64` and `JAVA_HOME_21_X64`. To install a JDK only for a
toolchain, without changing which `java` later steps run, set
`make-default-jdk: false`.

### JAVA_HOME

setup-java exports `JAVA_HOME` and the `JAVA_HOME_<major>_<ARCH>` variables
through `$BASH_ENV`, so they reach later steps that run in bash. Steps in
other shells, such as `sh` on Alpine or PowerShell on Windows, still find
`java` and `javac` on `PATH`, but do not see `JAVA_HOME`.

On Windows the paths are written with forward slashes (`C:/Users/...`), which
Git Bash and the JDK both read.

### Caching the install

The JDK is saved to the job cache after it is downloaded, and restored from
there by later jobs, so it is only downloaded once per version and platform.
`--cache=false` turns this off. A `jdkfile` install is never cached: the
archive is already on the machine.

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--distribution` | required | `temurin`, or `jdkfile` to install `--jdk-file` |
| `--java-version` | | Version to install, as above. Comma-separated to install several; the last is the default |
| `--java-version-file` | | File to read the version from: `.java-version`, `.tool-versions` or `.sdkmanrc` |
| `--java-package` | `jdk` | `jdk` or `jre` |
| `--architecture` | the machine's | `x64`, `x86`, `aarch64`, `armv7`, `ppc64le`, `ppc64`, `riscv64` or `s390x`. `amd64` and `arm64` work too |
| `--jdk-file` | | JDK archive to install, with `--distribution jdkfile` |
| `--make-default-jdk` | `true` | Make this JDK the one `java`, `javac` and `JAVA_HOME` resolve to. `false` installs it only for toolchains |
| `--cache` | `true` | Restore the JDK from the job cache, and save it after a download |

## Platform support

setup-java runs on Linux, macOS and Windows.

On Linux it detects whether the system uses glibc (Ubuntu, Debian, RHEL and
most others) or musl (Alpine), and installs a build for that C library.

Which builds exist depends on the distribution, the version and the
architecture. Temurin, for example, publishes musl builds for x64 and
aarch64 only, and no Java 8 for Apple silicon. When there is no build for the
machine, the step fails and says so.
