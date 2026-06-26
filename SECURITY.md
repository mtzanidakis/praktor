# Security Policy

## Reporting a Vulnerability

Please report security vulnerabilities **privately**. Do not open a public
issue, pull request, or discussion for security problems.

Preferred channel: **GitHub private vulnerability reporting**. Go to the
repository's **Security** tab → **Report a vulnerability**, or use the
[advisories page](https://github.com/mtzanidakis/praktor/security/advisories/new).

Alternatively, email **mtzanidakis@gmail.com** with:

- a description of the issue and its impact,
- the affected version or commit (`praktor version`),
- steps to reproduce (a minimal proof of concept is appreciated), and
- any suggested remediation.

## What to Expect

- **Acknowledgement** of your report within **5 business days**.
- A **triage assessment** (severity, affected versions) within **10 business days**.
- Coordinated disclosure: we will agree on a timeline with you before any public
  disclosure. Our default target is to ship a fix within **90 days** of triage,
  sooner for high-severity issues.
- **Credit**: reporters are credited in the release notes and/or the published
  advisory unless they prefer to remain anonymous.

## Scope

Praktor routes messages to Claude Code agents running in isolated Docker
containers. Reports that are especially in scope include:

- breaking the agent container / workspace isolation boundary,
- secret/vault exposure,
- authentication or authorization bypass in the Web UI or API,
- remote-triggerable code execution or injection (including prompt/instruction
  injection that escapes the intended task boundary).

Thank you for helping keep Praktor and its users safe.
