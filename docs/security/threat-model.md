# Threat model

## Protected assets

- Node bootstrap key and administrator TOTP
- device private keys and device credentials
- enrollment and relay grants
- wrapped content keys and encrypted file chunks
- transfer metadata, delivery state, and audit history

## Trust boundaries

The primary Node authorizes users, devices, transfers, and wrapped keys. Secondary relays are untrusted storage and transport endpoints: they may hold ciphertext but never receive administrator credentials, database credentials, Node keys, device private keys, or plaintext content.

Discovery, QR codes, deep links, and network reachability do not grant trust. Every route and relay request requires an authenticated device identity and a scoped grant.

## Principal threats and controls

- leaked Node key: use it only to issue short-lived enrollment grants; rotate without replacing device credentials
- token replay: bind grants to Node, operation, object, expiration, nonce, and maximum use count
- stolen device: revoke its independent credential and stop issuing wrapped keys to it
- malicious relay: end-to-end encryption, scoped grants, size limits, identity pinning, and hash verification
- path traversal: normalized relative folder paths, platform collision checks, and atomic writes
- log leakage: never log secrets, plaintext messages, filenames in push payloads, or wrapped keys
- deletion forgery: signed, versioned tombstones with authorization and retention windows

## Recovery assumptions

A lost Node key can be replaced while existing device credentials remain valid. A stolen trusted device requires credential revocation and review of content already downloaded by that device. Deletion cannot retract exported or copied plaintext from a receiver.
