# Troubleshoot

Start from the failure class on the work item; it's the main signal
the host passes through. The full class tables are in
[operate](operate.md).

## Troubleshooting

If everything comes back `registry_auth_denied`, check that the
`*_env` names in your config actually resolve in the worker, and for
ECR check the worker's AWS credentials, since those are minted per
claim. `registry_not_found` with a repository you're sure exists
usually means the `registry` and `repository` pair doesn't describe
the same repository the claim scope names. Repeated
`claim_deadline_exceeded` on a tag-heavy repository is a lease or
`tag_limit` problem, not a registry outage. And when the host only
gives you a class, run the binary locally with a recorded request so
you can read the stderr line.
