import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

test("設備使用節點連結與節點密鑰直接加入", async () => { const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8"); const api = await readFile(new URL("../src/api.ts", import.meta.url), "utf8"); assert.match(app, /nexdrop:\/\/join/); assert.doesNotMatch(app, /nexdrop:\/\/pair/); assert.match(api, /X-NexDrop-Node-Key/); });
test("聊天室支援拖放及通知", async () => { const app = await readFile(new URL("../src/App.tsx", import.meta.url), "utf8"); assert.match(app, /label: "聊天室"/); assert.match(app, /function ChatView/); assert.match(app, /onDrop=/); assert.match(app, /new Notification/); });
