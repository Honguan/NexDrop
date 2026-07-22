# NexDrop browser extension

[繁體中文](README.zh-TW.md)

The Manifest V3 extension supports Chrome and Edge. It is an independent NexDrop device, not an alias for the Windows desktop client, and therefore keeps its own identity and session.

## Pairing and daily use

1. Open **Pairing settings** and enter the HTTPS Node URL, device name, account, and password. Enter the six-digit verification code when TOTP is enabled.
2. Grant host access only for that Node. The extension creates a local X25519 key and registers an independent device.
3. Devices under the same account and Linux Node are trusted automatically. Only an existing pending device or a cross-node scenario requires approval from the **Devices** view.
4. Enter text in the popup, keep or remove the current tab URL, and select the receiving devices. Trusted devices are selected by default. Unsent text is kept locally, and `Ctrl/Command + Enter` sends without reaching for the button.

Tokens and the device private key remain in extension-local storage. Disconnecting locally does not remove the server record; revoke devices that are no longer used from NexDrop Web.

## Build

```powershell
npm ci
npm run lint
npm run typecheck
npm test
npm run build
```

Enable developer mode on the browser extensions page and load `dist/`. Run `npm run package` for separate Chrome and Edge archives under the repository `dist/` directory. Packages exclude `.env`, tokens, and certificates.

The current extension does not request Native Messaging or notification permissions and does not depend on a running desktop client.
