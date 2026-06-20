/**
 * Example: Optimize a PDF using pdfcpu WASM.
 *
 * Usage (from project root):
 *   bun run example/optimize.ts
 *
 * Requires a sample.pdf in the project root.
 */

// Resolve paths relative to this script's location
const ROOT = join(import.meta.dir, "..")

function join(...parts: string[]) {
  return parts.join("/").replace(/\/+/g, "/")
}

// Load Go WASM runtime glue
const wasmExecJs = await Bun.file(join(ROOT, "dist/wasm_exec.js")).text()
eval(wasmExecJs)

import { Pdfcpu } from "../pdfcpu.ts"

async function main() {
  // 1. Load pdfcpu WASM
  console.log("Loading pdfcpu WASM...")
  const wasmBytes = new Uint8Array(
    await Bun.file(join(ROOT, "dist/pdfcpu.wasm")).arrayBuffer(),
  )
  const pdfcpu = await Pdfcpu.create(wasmBytes)
  console.log("  ✓ pdfcpu ready")

  // 2. Read input PDF
  const pdfBytes = new Uint8Array(
    await Bun.file(join(ROOT, "example/sample1.pdf")).arrayBuffer(),
  )
  console.log(`  ✓ Read "sample1.pdf" (${pdfBytes.length} bytes)`)

  // 3. Get PDF info before optimization
  console.log("\n--- PDF Info ---")
  const infoResult = pdfcpu.info(pdfBytes)
  if (infoResult.ok) {
    const info = JSON.parse(infoResult.data!)
    console.log(`  Pages:    ${info.pageCount}`)
    console.log(`  Version:  ${info.version}`)
    console.log(`  Title:    ${info.title ?? "(none)"}`)
    console.log(`  Author:   ${info.author ?? "(none)"}`)
    console.log(`  Encrypted: ${info.encrypted}`)
  } else {
    console.error("  Info failed:", infoResult.error)
  }

  // 4. Optimize
  console.log("\n--- Optimizing ---")
  const result = pdfcpu.optimize(pdfBytes, { validationMode: "relaxed" })

  if (!result.ok) {
    console.error("  ✗ Optimization failed:", result.error)
    process.exit(1)
  }

  // 5. Write result
  await Bun.write(join(ROOT, "optimized.pdf"), result.data!)
  console.log(`  ✓ Wrote "optimized.pdf" (${result.data!.length} bytes)`)

  const saved = pdfBytes.length - result.data!.length
  const pct = ((saved / pdfBytes.length) * 100).toFixed(1)
  console.log(`  ✓ Saved ${saved} bytes (${pct}%)`)
}

await main()
