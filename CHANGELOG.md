# Changelog

All notable changes to setup-java will be documented in this file. Each release's section is added
by its release PR (see [RELEASE.md](RELEASE.md)) and becomes its GitHub release notes.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] - 2026-10-23

### What's Changed
* Stop passing a GitHub token to CI to download the toolkit by @stiyyagura0901 in https://github.com/circleci-functions/setup-java/pull/6
* PIE-189: Cache Maven and Gradle dependencies by @mangyau in https://github.com/circleci-functions/setup-java/pull/5


**Full Changelog**: https://github.com/circleci-functions/setup-java/compare/v0.2.0...v0.3.0

## [0.2.0] - 2026-10-16

### What's Changed
* PIE-189: Add Zulu, Corretto, Microsoft, Oracle OpenJDK and GraalVM by @mangyau in https://github.com/circleci-functions/setup-java/pull/3


**Full Changelog**: https://github.com/circleci-functions/setup-java/compare/v0.1.0...v0.2.0

## [0.1.0] - 2026-10-13

The first release: installs Eclipse Temurin JDKs, or a JDK from a local archive, on Linux (glibc
and musl), macOS and Windows.
