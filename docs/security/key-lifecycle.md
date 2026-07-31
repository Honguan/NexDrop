# Key lifecycle

## Node bootstrap key

The Node key is a root bootstrap secret. First-party clients use it only through an operator-facing flow that creates a short-lived enrollment token. Clients must delete the Node key after successful enrollment and must never send it in routine HTTP, WebSocket, or transfer requests.

Rotate the Node key when it is exposed, copied to an uncontrolled system, or after an administrator transition. Rotation invalidates new bootstrap attempts but does not revoke existing device credentials.

## Enrollment tokens

Tokens are integrity protected, bound to the Node identity, time limited, use limited, and optionally restricted by device type, name, owner, and permissions. Store only token hashes. Expired, revoked, or exhausted tokens return stable error codes and cannot be replayed.

## Device credentials

Every device receives an independent random credential and X25519 identity. Device credentials are revocable individually. Revocation ends sessions, prevents new wrapped content keys, rejects future relay grants, and notifies remaining trusted devices.

## Relay grants

Relay grants are short lived and bind relay, transfer, file, operation, byte limit, expiration, and nonce. Relay identity keys are pinned by the primary Node. Draining or removal stops new assignments without deleting verified active chunks prematurely.

## Emergency recovery

- lost Node key: replace it and issue new enrollment tokens
- lost administrator TOTP: use the documented offline administrative recovery procedure and rotate Node credentials
- stolen device: revoke the device, terminate sessions, inspect recent grant use, and rotate user secrets when compromise is suspected
- relay compromise: drain and remove the relay, revoke outstanding grants, and reassign encrypted chunks
