# Device enrollment protocol

## Flow

1. An authenticated administrator uses the Node bootstrap key locally to request an enrollment grant.
2. The Node returns a short-lived token or `nexdrop://join` link.
3. The new client submits the token, device metadata, and X25519 public key.
4. The Node atomically consumes one token use, creates the device identity, and returns an independent device credential.
5. The client stores the device credential in platform-protected storage and deletes the Node key and enrollment token.

Example:

```text
nexdrop://join?node=https%3A%2F%2Fdrop.example.com&token=<one-time-token>
```

## Grant restrictions

- expiration, maximum uses, and intended Node identity
- optional owner, device type, and name hint
- permission to send files
- permission to receive broadcast content

Concurrent redemption is serialized in PostgreSQL. Only the token hash is stored. Expired, exhausted, revoked, wrong-Node, wrong-device-type, and replay attempts use stable versioned errors.

## Revocation and rotation

A device credential is independent from the Node key and other devices. Revoking one device must not disconnect unrelated devices. Rotating the Node key affects only future enrollment grants.

## Legacy migration

The `scoped_device_enrollment` capability controls the new flow. Legacy 2.x clients may continue Node-key creation during the documented migration window. The UI must label that path as compatibility behavior and encourage re-enrollment.
