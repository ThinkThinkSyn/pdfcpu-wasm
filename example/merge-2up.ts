/**
 * Example: 2‑up each PDF, then merge them.
 *
 * Usage (from project root):
 *   bun run example/merge-2up.ts
 *
 * Requires sample1.pdf and sample2.pdf in the example/ folder.
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
  console.log("  ✓ pdfcpu ready\n")

  // 2. Read both PDFs
  const pdf1 = new Uint8Array(
    await Bun.file(join(ROOT, "example/sample1.pdf")).arrayBuffer(),
  )
  const pdf2 = new Uint8Array(
    await Bun.file(join(ROOT, "example/sample2.pdf")).arrayBuffer(),
  )
  console.log(`  Read sample1.pdf (${pdf1.length} bytes)`)
  console.log(`  Read sample2.pdf (${pdf2.length} bytes)\n`)

  // 3. Show original page counts
  const info1 = pdfcpu.info(pdf1)
  const info2 = pdfcpu.info(pdf2)
  const p1 = info1.ok ? JSON.parse(info1.data!).pageCount : 0
  const p2 = info2.ok ? JSON.parse(info2.data!).pageCount : 0
  console.log(`  sample1.pdf: ${p1} pages`)
  console.log(`  sample2.pdf: ${p2} pages\n`)

  // 4. 2‑up each PDF
  const nupCfg = { pageWidth: 842, pageHeight: 595, gridRows: 1, gridCols: 2, border: true, margin: 5 }

  console.log("--- 2‑up sample1.pdf ---")
  const r1 = pdfcpu.nup(pdf1, [], [], nupCfg)
  if (!r1.ok) { console.error("  ✗", r1.error); process.exit(1) }
  console.log(`  ✓ ${p1} pages → ${Math.ceil(p1 / 2)} sheets\n`)

  console.log("--- 2‑up sample2.pdf ---")
  const r2 = pdfcpu.nup(pdf2, [], [], nupCfg)
  if (!r2.ok) { console.error("  ✗", r2.error); process.exit(1) }
  console.log(`  ✓ ${p2} pages → ${Math.ceil(p2 / 2)} sheets\n`)

  // 5. Merge the two 2‑up results
  console.log("--- Merging ---")
  const merged = pdfcpu.merge([new Uint8Array(r1.data!), new Uint8Array(r2.data!)], false)
  if (!merged.ok) { console.error("  ✗ Merge failed:", merged.error); process.exit(1) }

  await Bun.write(join(ROOT, "merged-2up.pdf"), merged.data!)
  console.log(`  ✓ Wrote "merged-2up.pdf" (${merged.data!.length} bytes)`)

  const totalSheets = Math.ceil(p1 / 2) + Math.ceil(p2 / 2)
  console.log(`  Total: ${totalSheets} sheets`)
}

await main()
