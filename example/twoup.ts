/**
 * Example: 2-Up — put two PDF pages side by side (like a printer's 2‑up mode).
 *
 * Usage (from project root):
 *   bun run example/twoup.ts
 *
 * Requires sample.pdf in the project root.
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

  // 2. Read input PDF
  const pdfBytes = new Uint8Array(
    await Bun.file(join(ROOT, "example/sample1.pdf")).arrayBuffer(),
  )
  console.log(`  ✓ Read "sample1.pdf" (${pdfBytes.length} bytes)`)

  // 3. Show original page count
  console.log("\n--- Before ---")
  const info = pdfcpu.info(pdfBytes)
  if (info.ok) {
    const meta = JSON.parse(info.data!)
    console.log(`  Pages: ${meta.pageCount}`)
  }

  // 4. Apply 2‑up layout
  //    gridRows=1, gridCols=2 → two pages side by side on one A4 landscape sheet
  //    pageWidth/pageHeight set explicit landscape dimensions (A4 rotated)
  console.log("\n--- Applying 2‑up layout ---")
  const result = pdfcpu.nup(pdfBytes, [], [], {
    pageWidth: 842,
    pageHeight: 595,
    gridRows: 1,
    gridCols: 2,
    border: true,
    margin: 5,
  })

  if (!result.ok) {
    console.error("  ✗ 2‑up failed:", result.error)
    process.exit(1)
  }

  // 5. Write result
  await Bun.write(join(ROOT, "sample-2up.pdf"), result.data!)
  console.log(`  ✓ Wrote "sample-2up.pdf" (${result.data!.length} bytes)`)

  // 6. Show new page count
  const outInfo = pdfcpu.info(new Uint8Array(result.data!))
  if (outInfo.ok) {
    const meta = JSON.parse(outInfo.data!)
    console.log(`  Output pages: ${meta.pageCount} (${Math.ceil(meta.pageCount / 2)} sheets)`)
  }
}

await main()
