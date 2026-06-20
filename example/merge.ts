/**
 * Example: Merge two PDFs into one document.
 *
 * Usage (from project root):
 *   bun run example/merge.ts
 *
 * Requires sample.pdf in the example/ folder.
 */

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

  // 2. Read input PDFs
  const pdf1 = new Uint8Array(
    await Bun.file(join(ROOT, "example/sample1.pdf")).arrayBuffer(),
  )
  console.log(`  ✓ Read "sample1.pdf" (${pdf1.length} bytes)`)

  const pdf2 = new Uint8Array(
    await Bun.file(join(ROOT, "example/sample2.pdf")).arrayBuffer(),
  )
  console.log(`  ✓ Read "sample2.pdf" (${pdf2.length} bytes)`)

  // 3. Show original page counts
  const info1 = pdfcpu.info(pdf1)
  const info2 = pdfcpu.info(pdf2)
  let pages1 = 0, pages2 = 0
  if (info1.ok) pages1 = JSON.parse(info1.data!).pageCount
  if (info2.ok) pages2 = JSON.parse(info2.data!).pageCount
  console.log(`  sample1.pdf: ${pages1} pages`)
  console.log(`  sample2.pdf: ${pages2} pages`)

  // 4. Merge the two PDFs together (A+B)
  console.log("\n--- Merging ---")
  console.log(`  Merging sample1.pdf + sample2.pdf (${pages1} + ${pages2} pages)`)
  const result = pdfcpu.merge([pdf1, pdf2], false)

  if (!result.ok) {
    console.error("  ✗ Merge failed:", result.error)
    process.exit(1)
  }

  // 5. Write result
  await Bun.write(join(ROOT, "sample-merged.pdf"), result.data!)
  console.log(`  ✓ Wrote "sample-merged.pdf" (${result.data!.length} bytes)`)

  // 6. Show merged page count
  const outInfo = pdfcpu.info(new Uint8Array(result.data!))
  if (outInfo.ok) {
    const meta = JSON.parse(outInfo.data!)
    console.log(`  Total pages: ${meta.pageCount}`)
  }
}

await main()
