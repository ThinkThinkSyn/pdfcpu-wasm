/**
 * pdfcpu WASM — Complete TypeScript bindings
 *
 * Covers all 65+ pdfcpu operations compiled to WebAssembly.
 *
 * ## Quick start
 *
 * ```typescript
 * import { Pdfcpu } from "./pdfcpu.ts";
 * const pdfcpu = await Pdfcpu.create("/pdfcpu.wasm");
 *
 * const pdf = new Uint8Array(await (await fetch("doc.pdf")).arrayBuffer());
 * const opt = pdfcpu.optimize(pdf); // { ok, data?, error? }
 * ```
 *
 * ## Prerequisites
 *
 * Load `wasm_exec.js` BEFORE this module:
 * ```html
 * <script src="/wasm_exec.js"></script>
 * <script type="module" src="app.ts"></script>
 * ```
 */

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Result returned by every pdfcpu operation. */
export interface PdfcpuResult<T = Uint8Array | string | number> {
  ok: boolean;
  data?: T;
  error?: string;
}

/** Optional configuration passed to pdfcpu operations. */
export interface PdfcpuConfig {
  validationMode?: "relaxed" | "strict";
  unit?: "points" | "inches" | "cm" | "mm";
  ownerPW?: string;
  userPW?: string;
  encryptKeyLength?: 40 | 128 | 256;
  permissions?: number;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function enc(s?: string): Uint8Array | undefined {
  return s ? new TextEncoder().encode(s) : undefined;
}

function encObj(obj?: unknown): Uint8Array | undefined {
  return obj ? new TextEncoder().encode(JSON.stringify(obj)) : undefined;
}

function g(): any {
  return globalThis as any;
}

// ---------------------------------------------------------------------------
// Pdfcpu Client
// ---------------------------------------------------------------------------

export class Pdfcpu {
  ready = false;

  private constructor() {}

  /**
   * Load WASM and initialise the Go runtime.
   *
   * `wasmSource` can be a URL string, `ArrayBuffer`, or `Uint8Array`.
   * Requires the `Go` class (from `wasm_exec.js`) to be globally available.
   */
  static async create(wasmSource: string | ArrayBuffer | Uint8Array): Promise<Pdfcpu> {
    const Go = g().Go;
    if (!Go) throw new Error("Go WASM runtime not found. Load wasm_exec.js first.");

    const go = new Go();
    const api = new Pdfcpu();

    if (typeof wasmSource === "string") {
      const resp = await fetch(wasmSource);
      if (!resp.ok) throw new Error(`Failed to fetch WASM: ${resp.status}`);
      const result = await WebAssembly.instantiateStreaming(resp, go.importObject);
      go.run(result.instance);
    } else {
      const result = await WebAssembly.instantiate(wasmSource, go.importObject);
      go.run(result.instance);
    }

    await new Promise((r) => setTimeout(r, 0));
    api.ready = g().pdfcpu_ready === true;
    if (!api.ready) throw new Error("pdfcpu WASM failed to initialise");
    return api;
  }

  // ====================================================================
  //  DOCUMENT-LEVEL
  // ====================================================================

  /** Validate a PDF against ISO-32000. */
  validate(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_validate(pdf, encObj(config));
  }

  /** Optimize a PDF (compress streams, remove unused objects). Returns optimized PDF bytes. */
  optimize(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_optimize(pdf, encObj(config));
  }

  /** Extract PDF information (metadata, pages, fonts). Returns JSON string. */
  info(pdf: Uint8Array, selectedPages?: string[], fonts?: boolean, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_info(pdf, selectedPages ?? [], fonts ?? false, encObj(config));
  }

  /** Get page count. */
  pageCount(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<number> {
    return g().pdfcpu_pageCount(pdf, encObj(config));
  }

  /** Get page dimensions. Returns JSON string. */
  pageDims(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_pageDims(pdf, encObj(config));
  }

  // ====================================================================
  //  PAGE OPERATIONS
  // ====================================================================

  /** Trim a PDF to keep only selected pages. */
  trim(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_trim(pdf, pages, encObj(config));
  }

  /** Collect pages into a custom order (supports duplicates). */
  collect(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_collect(pdf, pages, encObj(config));
  }

  /** Rotate pages clockwise by `rotation` degrees (90, 180, 270). */
  rotate(pdf: Uint8Array, rotation?: number, pages?: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_rotate(pdf, rotation ?? 90, pages ?? [], encObj(config));
  }

  /** Insert blank pages before specified pages. */
  insertPages(pdf: Uint8Array, pages: string[], before: boolean, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_insertPages(pdf, pages, before, encObj(config));
  }

  /** Remove specified pages. */
  removePages(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removePages(pdf, pages, encObj(config));
  }

  /** Crop pages to a rectangle. `box` is `{rect: [x1,y1,x2,y2]}`. */
  crop(pdf: Uint8Array, pages: string[], box: { rect: [number, number, number, number] }, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_crop(pdf, pages, encObj(box), encObj(config));
  }

  /** Add page boundaries (boxes). `boxes` is `{mediaBox?: {}, cropBox?: {}, trimBox?: {}, bleedBox?: {}, artBox?: {}}`. */
  addBoxes(pdf: Uint8Array, pages: string[], boxes: Record<string, unknown>, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_addBoxes(pdf, pages, encObj(boxes), encObj(config));
  }

  /** Remove page boundaries. `boxes` is `{cropBox?: {}, trimBox?: {}, bleedBox?: {}, artBox?: {}}`. */
  removeBoxes(pdf: Uint8Array, pages: string[], boxes: Record<string, unknown>, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removeBoxes(pdf, pages, encObj(boxes), encObj(config));
  }

  // ====================================================================
  //  SECURITY
  // ====================================================================

  /** Encrypt a PDF (set passwords/permissions via config). */
  encrypt(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_encrypt(pdf, encObj(config));
  }

  /** Decrypt a PDF (provide passwords via config). */
  decrypt(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_decrypt(pdf, encObj(config));
  }

  /** Change user password. */
  changeUserPassword(pdf: Uint8Array, pwOld: string, pwNew: string, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_changeUserPassword(pdf, pwOld, pwNew, encObj(config));
  }

  /** Change owner password. */
  changeOwnerPassword(pdf: Uint8Array, pwOld: string, pwNew: string, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_changeOwnerPassword(pdf, pwOld, pwNew, encObj(config));
  }

  /** Get permissions as a number. */
  getPermissions(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<number> {
    return g().pdfcpu_getPermissions(pdf, encObj(config));
  }

  /** Set permissions (set `permissions` in config). Alias for convenience. */
  setPermissions(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_setPermissions(pdf, encObj(config));
  }

  /** Get permissions as a plain integer (alternative to getPermissions). */
  permissions(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<number> {
    return g().pdfcpu_permissions(pdf, encObj(config));
  }

  // ====================================================================
  //  CONTENT / WATERMARKS / ANNOTATIONS
  // ====================================================================

  /** Add text watermarks. `wm` supports `{text, fontSize?, opacity?, onTop?, rotation?, scale?}`. */
  addWatermarks(pdf: Uint8Array, pages: string[], wm: Record<string, unknown>, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_addWatermarks(pdf, pages, encObj(wm), encObj(config));
  }

  /** Remove watermarks from pages. */
  removeWatermarks(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removeWatermarks(pdf, pages, encObj(config));
  }

  /** Check if a PDF has watermarks. Returns boolean. */
  hasWatermarks(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<boolean> {
    return g().pdfcpu_hasWatermarks(pdf, encObj(config));
  }

  /** List annotations. Returns JSON string. */
  listAnnotations(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_listAnnotations(pdf, pages, encObj(config));
  }

  /** Remove annotations. */
  removeAnnotations(pdf: Uint8Array, pages: string[], idsAndTypes: string[], objNrs: number[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removeAnnotations(pdf, pages, idsAndTypes, objNrs, encObj(config));
  }

  // ====================================================================
  //  BOOKMARKS
  // ====================================================================

  /** List bookmarks. Returns JSON string. */
  listBookmarks(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_listBookmarks(pdf, encObj(config));
  }

  /** Add bookmarks. `bookmarks` is a JSON array of bookmark objects. */
  addBookmarks(pdf: Uint8Array, bookmarks: string, replace: boolean, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_addBookmarks(pdf, bookmarks, replace, encObj(config));
  }

  /** Remove all bookmarks. */
  removeBookmarks(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removeBookmarks(pdf, encObj(config));
  }

  /** Export bookmarks as JSON. Returns JSON string. */
  exportBookmarks(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_exportBookmarks(pdf, encObj(config));
  }

  /** Import bookmarks from JSON string. */
  importBookmarks(pdf: Uint8Array, bookmarksJSON: string, replace: boolean, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_importBookmarks(pdf, bookmarksJSON, replace, encObj(config));
  }

  // ====================================================================
  //  ATTACHMENTS
  // ====================================================================

  /** List embedded file attachments. Returns JSON string. */
  listAttachments(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_listAttachments(pdf, encObj(config));
  }

  /** Add file attachments. */
  addAttachments(pdf: Uint8Array, fileNames: string[], asPortfolio: boolean, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_addAttachments(pdf, fileNames, asPortfolio, encObj(config));
  }

  /** Remove embedded file attachments. */
  removeAttachments(pdf: Uint8Array, fileNames: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removeAttachments(pdf, fileNames, encObj(config));
  }

  // ====================================================================
  //  FORM FIELDS
  // ====================================================================

  /** List form fields. Returns JSON string. */
  listFormFields(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_listFormFields(pdf, encObj(config));
  }

  /** Remove form fields. */
  removeFormFields(pdf: Uint8Array, fieldIDsOrNames: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removeFormFields(pdf, fieldIDsOrNames, encObj(config));
  }

  /** Lock form fields. */
  lockFormFields(pdf: Uint8Array, fieldIDsOrNames: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_lockFormFields(pdf, fieldIDsOrNames, encObj(config));
  }

  /** Unlock form fields. */
  unlockFormFields(pdf: Uint8Array, fieldIDsOrNames: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_unlockFormFields(pdf, fieldIDsOrNames, encObj(config));
  }

  /** Reset form fields to default values. */
  resetFormFields(pdf: Uint8Array, fieldIDsOrNames: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_resetFormFields(pdf, fieldIDsOrNames, encObj(config));
  }

  // ====================================================================
  //  KEYWORDS & PROPERTIES
  // ====================================================================

  /** List keywords. Returns JSON string. */
  listKeywords(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_listKeywords(pdf, encObj(config));
  }

  /** Add keywords. */
  addKeywords(pdf: Uint8Array, keywords: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_addKeywords(pdf, keywords, encObj(config));
  }

  /** Remove keywords. */
  removeKeywords(pdf: Uint8Array, keywords: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removeKeywords(pdf, keywords, encObj(config));
  }

  /** List document properties. Returns JSON string. */
  listProperties(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_listProperties(pdf, encObj(config));
  }

  /** Add document properties. `props` is `{key: value, ...}`. */
  addProperties(pdf: Uint8Array, props: Record<string, string>, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_addProperties(pdf, encObj(props), encObj(config));
  }

  /** Remove document properties by name. */
  removeProperties(pdf: Uint8Array, propNames: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removeProperties(pdf, propNames, encObj(config));
  }

  // ====================================================================
  //  LAYOUT / METADATA
  // ====================================================================

  /** Get page layout. Returns JSON string. */
  getPageLayout(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_getPageLayout(pdf, encObj(config));
  }

  /** Set page layout. `layout` is a PageLayout value string. */
  setPageLayout(pdf: Uint8Array, layout: string, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_setPageLayout(pdf, layout, encObj(config));
  }

  /** Reset page layout to default. */
  resetPageLayout(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_resetPageLayout(pdf, encObj(config));
  }

  /** Get page mode. Returns JSON string. */
  getPageMode(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_getPageMode(pdf, encObj(config));
  }

  /** Set page mode. `mode` is a PageMode value string. */
  setPageMode(pdf: Uint8Array, mode: string, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_setPageMode(pdf, mode, encObj(config));
  }

  /** Reset page mode to default. */
  resetPageMode(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_resetPageMode(pdf, encObj(config));
  }

  /** Get viewer preferences. Returns JSON string. */
  getViewerPreferences(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_getViewerPreferences(pdf, encObj(config));
  }

  /** Set viewer preferences from JSON. */
  setViewerPreferences(pdf: Uint8Array, prefsJSON: string, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_setViewerPreferences(pdf, prefsJSON, encObj(config));
  }

  /** Reset viewer preferences to default. */
  resetViewerPreferences(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_resetViewerPreferences(pdf, encObj(config));
  }

  // ====================================================================
  //  MERGE / SPLIT
  // ====================================================================

  /** Merge multiple PDFs into one document. */
  merge(pdfs: Uint8Array[], dividerPage?: boolean, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_merge(pdfs, dividerPage ?? false, encObj(config));
  }

  /** Merge two PDFs into a zip container. */
  mergeZip(pdf1: Uint8Array, pdf2: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_mergeZip(pdf1, pdf2, encObj(config));
  }

  // ====================================================================
  //  LAYOUT OPERATIONS (N-Up, Booklet, Zoom, Resize)
  // ====================================================================

  /** N-Up layout. `nupConfig` is `{pageSize: "A4", ...}`. */
  nup(pdf: Uint8Array, imgFileNames: string[], pages: string[], nupConfig: Record<string, unknown>, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_nup(pdf, imgFileNames, pages, encObj(nupConfig), encObj(config));
  }

  /** Booklet layout. `bkConfig` is `{pageSize: "A4", ...}`. */
  booklet(pdf: Uint8Array, imgFileNames: string[], pages: string[], bkConfig: Record<string, unknown>, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_booklet(pdf, imgFileNames, pages, encObj(bkConfig), encObj(config));
  }

  /** Zoom pages. `zoomConfig` is `{factor: 1.5}`. */
  zoom(pdf: Uint8Array, pages: string[], zoomConfig: Record<string, unknown>, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_zoom(pdf, pages, encObj(zoomConfig), encObj(config));
  }

  /** Resize pages. `resizeConfig` is `{scale: 0.8, pageSize: "A4"}`. */
  resize(pdf: Uint8Array, pages: string[], resizeConfig: Record<string, unknown>, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_resize(pdf, pages, encObj(resizeConfig), encObj(config));
  }

  // ====================================================================
  //  SIGNATURES
  // ====================================================================

  /** Remove digital signatures. */
  removeSignatures(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return g().pdfcpu_removeSignatures(pdf, encObj(config));
  }

  // ====================================================================
  //  EXTRACTION
  // ====================================================================

  /** Extract images. Returns JSON string with base64-encoded image data. */
  extractImages(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_extractImages(pdf, pages, encObj(config));
  }

  /** Extract fonts. Returns JSON string. */
  extractFonts(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_extractFonts(pdf, pages, encObj(config));
  }

  /** Extract pages as individual PDF buffers. Returns array of `{pageNr, data: Uint8Array}`. */
  extractPagesReader(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): any {
    return g().pdfcpu_extractPagesReader(pdf, pages, encObj(config));
  }

  /** Extract page content text. Returns JSON string. */
  extractContent(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_extractContent(pdf, pages, encObj(config));
  }

  /** Extract metadata. Returns JSON string. */
  extractMetadata(pdf: Uint8Array, config?: PdfcpuConfig): PdfcpuResult<string> {
    return g().pdfcpu_extractMetadata(pdf, encObj(config));
  }

  // ====================================================================
  //  CONVENIENCE
  // ====================================================================

  /** Alias: extract specific pages into a new PDF (uses `collect`). */
  extractPages(pdf: Uint8Array, pages: string[], config?: PdfcpuConfig): PdfcpuResult<Uint8Array> {
    return this.collect(pdf, pages, config);
  }
}
