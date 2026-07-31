# Folder manifest protocol

A folder transfer uses a versioned manifest instead of a temporary archive. Every regular file remains independently resumable and verifiable.

## Version 1 entries

- directory, including empty directories
- regular file with normalized relative path, size, modification time, SHA-256, optional MIME type, and transfer file identifier

Symbolic links are not followed or represented in version 1.

## Path security

Reject absolute and drive-qualified paths, NUL bytes, `..` traversal, Windows reserved names, trailing spaces or dots, paths over configured length, excess nesting, and collisions after separator normalization or case folding. The receiving client validates the manifest again before writing.

## Limits

Default limits are 100,000 entries, 1,024 bytes per path, 64 levels, 32 MiB encoded manifest, and 16 TiB total declared content. Implementations may configure lower values.

## Selection and conflicts

The receiver may accept all, reject all, or select entries before body transfer. Required parent directories are retained. Conflict actions are overwrite, rename, skip, or ask. Existing files are skipped automatically only when both size and SHA-256 match.

## Resume and finalization

Each file uses existing verified chunks. Partial failure does not invalidate completed files. Write through temporary files and atomically publish verified content where supported. A folder cancellation stops remaining work and preserves completed files according to the receiver policy.

## Compatibility

Without mutual `folder_manifest` support, return a clear unsupported-feature result. Archive fallback is allowed only when explicitly selected by the sender.
