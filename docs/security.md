# Security

Report a suspected vulnerability privately with the "Report a vulnerability" button on the project's Security tab (GitHub private vulnerability reporting): https://github.com/amitpatole/cgroupguard/security/advisories/new
Do not include credentials, kubeconfigs or private cluster reports in public issues.

cgroupguard is read-only by default, treats everything it reads from a cluster as untrusted input, keeps credentials out of its output and
logs, and asks for explicit consent before anything that changes state. Releases are signed; see [Install](install.md#verify-what-you-downloaded).

## Platform notes

- On Linux and macOS the assistant audit log is created exclusively with owner-only permissions (0600). Windows has no POSIX file modes, so the log inherits the access rules of the folder it is
  written to; choose a private folder for `--audit-log` on Windows.
