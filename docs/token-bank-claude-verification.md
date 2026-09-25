# TokenBank Claude subscription verification

Claude owner submissions are verified against the fixed official OAuth profile
endpoint, `https://api.anthropic.com/api/oauth/profile`, using the submitted OAuth
access token. Refresh-only submissions first use the existing OAuth refresh flow.
The verifier reuses the existing Claude OAuth HTTP client factory, bounds the
profile response and request duration, and rejects redirects.

The selected `organization.organization_type` must be `claude_pro` or
`claude_max`. Account and organization UUIDs and account email must be present.
Submitted identity selectors must agree with that response. Account-wide
`has_claude_pro` / `has_claude_max` flags, rate-limit tiers, and submitted labels
are not evidence of the selected organization's personal subscription.

## Primary implementation evidence

Read-only inspection of the locally installed official Claude Code executable
`/Users/wade/.local/share/claude/versions/2.1.281` on 2026-09-25 found:

- Build timestamp `2026-09-23T02:22:43Z`, Git SHA
  `3e320108de6831eb996e9a3f7795152073cc0d0c`.
- The production API origin is `https://api.anthropic.com`.
- Its OAuth profile request uses Bearer authentication, JSON content type,
  `Cache-Control: no-cache`, and a 10-second timeout.
- Its profile validator requires `account.uuid`, `account.email`, and
  `organization.uuid`.
- It maps `organization.organization_type` values `claude_pro`, `claude_max`,
  `claude_team`, and `claude_enterprise` to the corresponding subscription types.

This is an official-client implementation detail, not a published stable API
contract. Missing profile scope, unexpected response shape, network failure, or
an unsupported organization denies eligibility. No live provider credentials
were read or used during this implementation.

Verification establishes the selected organization's subscription identity. It
does not guarantee that a later inference request avoids optional extra usage
charges or that subscription allowance remains available.
