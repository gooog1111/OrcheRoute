import { cp, mkdir, rm, stat } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const source = path.resolve(here, "../../webui/out");
const destination = path.resolve(here, "../frontend/dist");
const sourceInfo = await stat(source);
if (!sourceInfo.isDirectory()) throw new Error(`WebUI export not found: ${source}`);
await rm(destination, { recursive: true, force: true });
await mkdir(destination, { recursive: true });
await cp(source, destination, { recursive: true });
console.log(`Desktop frontend synced: ${destination}`);
