# ADR-004: Web and desktop bridge

[繁體中文](004-desktop-bridge.zh-TW.md)

Status: Superseded by [ADR-005](005-independent-extension.md)

Context: The original browser extension handed page content to an authenticated desktop client.

Decision: The original extension called `nexdrop-bridge` through Native Messaging. The bridge connected to a desktop process that listened only locally and required a short-lived token.

Consequences: That design required separate native-host registration for Chrome and Edge. The current extension no longer uses this bridge.
