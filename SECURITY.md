# Security policy

TQL is an early 0.x project. Please use the latest release; older 0.x releases do
not have a separate maintenance or security-backport commitment.

For a suspected vulnerability, use the repository's **Security → Report a
vulnerability** form when private reporting is available. Include the TQL version,
operating system, minimal input and query, and the observed impact. Do not include
credentials or private configuration files.

If private reporting is unavailable, open an issue asking for a private reporting
channel without publishing exploit details or sensitive input. No response-time
guarantee is currently provided.

The parsers enforce nesting and alias-expansion limits, and the VM limits
execution steps. These limits are not a memory or time sandbox. Applications
processing untrusted documents or queries should impose their own resource limits.
