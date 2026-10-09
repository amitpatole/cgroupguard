# Contributing

Thank you for helping. Use Go 1.27.2 or newer and run `gofmt`, `go vet ./...` and `go test -race ./...` before opening a pull request.

- Keep changes small, with tests, and explain behavior and any permissions involved.
- Every factual compatibility rule needs a current primary-source URL, a test for each supported boundary, and an explicit *unknown* path.
  Never turn missing information into a passing result.
- Dependency changes go in `go.mod`/`go.sum`; new dependencies need a permissive license.
- Do not include credentials, kubeconfigs or real cluster reports in issues or pull requests.

By contributing you agree that your contribution is licensed under the Apache License 2.0. Accepted changes are merged by the maintainer, who
reviews security, RBAC and source accuracy first. Report security problems privately, see [SECURITY.md](SECURITY.md).
