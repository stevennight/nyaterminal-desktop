# Security policy

Please do not open a public issue for a suspected vulnerability. Report it
privately to the project maintainers with reproduction steps and affected
versions.

## Invariants

- No credentials, private keys, command history, or connection metadata may be
  written to logs.
- New cryptographic constructions are prohibited. Use the reviewed primitives in
  `golang.org/x/crypto`.
- Authentication and unlock failures must not leak sensitive account, device, or
  vault state.
- SSH host-key verification must never silently fall back to an insecure mode.
- Synchronization records must remain end-to-end encrypted before they leave the
  desktop process.
