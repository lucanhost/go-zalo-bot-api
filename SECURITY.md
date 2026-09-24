# Security Policy

## Supported versions

The latest tagged release on `main` receives security fixes.

## Reporting a vulnerability

Please **do not** report security vulnerabilities through public GitHub
issues, discussions, or pull requests.

Instead, use GitHub's private vulnerability reporting: open the repository's
**Security** tab and choose **Report a vulnerability**. If that is not
available, contact the maintainer directly.

Please include:

- a description of the issue and its impact,
- steps to reproduce or a proof of concept,
- the affected version or commit, and
- any suggested mitigation.

You can expect an acknowledgement within a few days and a coordinated fix and
disclosure once the issue is confirmed.

## Handling bot tokens

A Zalo bot token grants full control of the bot. Treat it as a secret:

- Never commit a token, print it in logs, or include it in bug reports.
- The SDK redacts the token from transport errors. If you find any path that
  leaks a token in an error message, log line, or panic, that is a security
  bug — report it privately.
