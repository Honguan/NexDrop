# Client compatibility matrix

[繁體中文](compatibility-matrix.zh-TW.md)

Current protocol: `1.2`

Compatibility contract fingerprint: `030186dbb2fae08802c053d69a56a76c24c6e9d418984200c4a1ef36d494e4dd`

The Node supports the current protocol and two previous client protocol generations. Capability negotiation is additive: a missing advertisement produces an empty intersection and activates the documented fallback.

LAN discovery continues to advertise the compatible `1.1` baseline so 1.1 clients can discover current devices; authenticated status negotiation upgrades optional behavior to protocol 1.2 capabilities.

| Node | Client | Connection | Optional capabilities | Required behavior |
| --- | --- | --- | --- | --- |
| 1.2 | 1.2 | Supported | Negotiated intersection | Enable only mutually advertised behavior. |
| 1.2 | 1.1 | Supported | None | Use protocol 1.1 behavior and legacy fallbacks. |
| 1.2 | 1.0 or `1` | Supported | None | Use protocol 1.0 behavior and legacy fallbacks. |
| 1.1 | 1.2 | Supported | None | The client accepts a legacy version document and disables optional behavior. |
| 1.0 | 1.2 | Supported | None | The client accepts unknown-field absence and uses protocol 1.0 behavior. |
| Earlier or later unsupported protocol | Any | Rejected | None | Return the stable protocol-upgrade result. |

Current first-party clients advertise protocol 1.2. Web, Extension, Android, and Windows ignore unknown additive fields and unknown capability identifiers.

| First-party client | Current generation | Previous generation | Legacy generation | Capability behavior |
| --- | --- | --- | --- | --- |
| Windows Desktop | `nexdrop-v1.2` | `nexdrop-v1.1` | `nexdrop-v1.0` | Uses the Node document to select a common handshake and disables unavailable capabilities. |
| Android | `nexdrop-v1.2` | `nexdrop-v1.1` | `nexdrop-v1.0` | Uses the same Flutter contract and safe background fallback. |
| Web | `web-v1.2` | `web-v1.1` | `web-v1.0` | Refreshes the same-origin Node document before opening realtime transport. |
| Chrome/Edge Extension | `extension-v1.2` | `extension-v1.1` | `extension-v1.0` | Refreshes the paired Node document and never activates capabilities from an unverified cache. |

## Deprecation and removal

1. Additive capabilities may ship without changing the API version.
2. A capability scheduled for removal remains documented for at least two supported client generations.
3. The Node preserves the current and two previous protocol generations unless a security-critical minimum-client rule requires an earlier removal.
4. A breaking wire-format change requires a protocol version update, mixed-version fixtures, release notes, and a matrix update.
5. Removed capability identifiers remain reserved forever.

Release documentation validation reads the protocol constant and capability registry. It fails when this matrix omits the current protocol or the capability document omits a registered identifier.
