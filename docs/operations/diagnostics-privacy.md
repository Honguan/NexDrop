# Diagnostic privacy and redaction

[繁體中文](diagnostics-privacy.zh-TW.md)

Diagnostic collection follows data minimization:

- Configuration keys containing `PASSWORD`, `TOKEN`, `SECRET`, `KEY`, or `CREDENTIAL` are retained only as `[REDACTED]`.
- User information in URL credentials, bearer authorization values, and sensitive query parameters is removed.
- Known secret values are scrubbed from health details before serialization.
- The bundle uses structured JSON entries; it never copies arbitrary logs, environment dumps, message bodies, filenames, indexes, or stored files.
- Resource counts and bounded status/error codes are allowed. Raw user, device, file, message, request, and transfer identifiers are excluded from metric labels.

Automated tests open the generated ZIP and assert that representative password, token, URL-credential, and bearer-token formats are absent. A new diagnostic entry must add equivalent negative tests before it can be collected.
