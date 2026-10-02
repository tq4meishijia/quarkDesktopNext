// 前端语法自检：以 ESM 严格模式解析所有 .js（node --check 对含 import 的 .js
// 不会按 ESM 解析，必须当作 .mjs 才能可靠检出语法错误）。本脚本常驻 scripts/，
// 用法：node scripts/check-frontend-syntax.mjs
import { readdirSync, statSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import vm from "node:vm";

const ROOT = new URL("..", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1");
const FRONTEND = join(ROOT, "frontend");

function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (name.endsWith(".js")) out.push(p);
  }
  return out;
}

const files = walk(FRONTEND).sort();
let bad = 0;
for (const f of files) {
  const code = readFileSync(f, "utf8");
  try {
    new vm.SourceTextModule(code, { identifier: relative(ROOT, f) });
  } catch (e) {
    bad++;
    console.error(`❌ ${relative(ROOT, f)}: ${e.message}`);
  }
}
console.log(`已检查 ${files.length} 个 JS 文件（ESM 严格模式）`);
console.log(bad === 0 ? "✅ 前端语法全部通过" : `❌ ${bad} 个文件存在语法错误`);
process.exit(bad === 0 ? 0 : 1);
