import { cp, mkdir, rm } from "node:fs/promises";

await mkdir("dist", { recursive: true });
for (const file of ["manifest.json", "popup.html", "popup.css", "options.html", "icon128.png"]) {
  await cp(file, `dist/${file}`);
}
await rm("dist/scripts", { recursive: true, force: true });
