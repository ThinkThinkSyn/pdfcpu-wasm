# @thinkthinksyn/pdfcpu-wasm

Standalone npm package for the [pdfcpu](https://github.com/pdfcpu/pdfcpu)
WebAssembly build. It exposes the full pdfcpu API (validate, optimize, merge,
split, encrypt, watermark, extract, etc.) to TypeScript/JavaScript, entirely
in-memory.

## Install

```bash
npm install @thinkthinksyn/pdfcpu-wasm
```

## Usage

```typescript
import { Pdfcpu } from "@thinkthinksyn/pdfcpu-wasm/pdfcpu.ts";

const pdfcpu = await Pdfcpu.create("/pdfcpu.wasm");

const pdf = new Uint8Array(await (await fetch("doc.pdf")).arrayBuffer());

const opt = pdfcpu.optimize(pdf);
if (opt.ok) {
  // opt.data is a Uint8Array with the optimized PDF
}
```

See [`example/optimize.ts`](./example/optimize.ts) for a complete Deno/Node example.

## Build locally

```bash
# Requires Go 1.25+
./build.sh
```

Output lands in `dist/`:

| File | Purpose |
|---|---|
| `pdfcpu.wasm` | Compiled WASM binary (~19 MB / ~5 MB gzip'd) |
| `wasm_exec.js` | Go WASM runtime glue |
| `version.json` | pdfcpu dependency version and build metadata |

## Automated upstream updates

`.github/workflows/update.yml` runs daily and on manual dispatch. It:

1. Queries the latest stable release of `pdfcpu/pdfcpu`
2. Compares it with the version currently packaged
3. If newer, bumps `go.mod` / `go.sum`
4. Rebuilds the WASM binary
5. Bumps `package.json` version to match the upstream release
6. Publishes `@thinkthinksyn/pdfcpu-wasm` to npm
7. Commits the updated files back to this repo

### Setup

1. Create the npm package `@thinkthinksyn/pdfcpu-wasm` at https://www.npmjs.com/.
2. Add an `NPM_TOKEN` secret to this repo:
   - Generate a **Granular Access Token** with **Read and write** access to
     `@thinkthinksyn/pdfcpu-wasm` and **Bypass two-factor authentication** enabled.
   - Add it at `Settings → Secrets and variables → Actions → New repository secret`.
3. Make sure the default `GITHUB_TOKEN` has **Read and write permissions**
   (Settings → Actions → General → Workflow permissions).

### Manual run

```bash
gh workflow run update.yml -f pdfcpu_version=v0.13.0 -f dry_run=true
```

This checks out the specified upstream version, builds the WASM, but skips npm
publish and the git push.

## Repo layout

```
.
├── .github/workflows/update.yml   # Upstream update automation
├── main.go                         # Go WASM entry point
├── pdfcpu.ts                       # TypeScript bindings
├── build.sh                        # Local build script
├── package.json                    # npm metadata
├── go.mod / go.sum                 # Go module files
├── dist/                           # Build output (tracked for npm/distribution)
└── example/optimize.ts             # Usage example
```

## License

Apache-2.0 (same as pdfcpu).
