// 前端语法自检：以 ESM 严格模式解析所有 .js（node --check 对含 import 的 .js
// 不会按 ESM 解析，必须走 vm.SourceTextModule 才能可靠检出语法错误）。
//
// 用法：
//   node --experimental-vm-modules scripts/check-frontend-syntax.mjs
//
// 为什么需要那个标志：vm.SourceTextModule 在 Node 22 仍是实验 API，
// 不带标志时它是 undefined，脚本会对每个文件报
// “vm.SourceTextModule is not a constructor”，看起来像全量语法错误，
// 实则一个都没检查。本脚本会先探测可用性，缺失时直接以非零码退出并说明原因，
// 避免这种「假失败 / 假通过」悄悄混进 CI。
import { readdirSync, statSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";
import vm from "node:vm";

const ROOT = new URL("..", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1");
const FRONTEND = join(ROOT, "frontend");

if (typeof vm.SourceTextModule !== "function") {
  console.error(
    [
      "❌ 无法执行前端语法自检：当前 Node 缺少 vm.SourceTextModule。",
      "",
      "该 API 仍是实验特性，需要显式开启标志。请改为：",
      "  node --experimental-vm-modules scripts/check-frontend-syntax.mjs",
      "",
      "注意：不带标志运行时本脚本会「检查了 0 个错误」或报全部文件错误，",
      "两者都不可信，故这里直接失败退出，避免假门禁。",
    ].join("\n"),
  );
  process.exit(2);
}

function walk(dir) {
  const out = [];
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) out.push(...walk(p));
    else if (name.endsWith(".js")) out.push(p);
  }
  return out;
}

let files = [];
try {
  files = walk(FRONTEND).sort();
} catch (e) {
  console.error(`❌ 无法遍历前端目录 ${FRONTEND}: ${e.message}`);
  process.exit(2);
}
if (files.length === 0) {
  console.error(`❌ ${FRONTEND} 下未找到任何 .js 文件，疑似路径异常`);
  process.exit(2);
}

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
