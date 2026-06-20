//go:build js

/*
Copyright 2026 The pdfcpu Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package main is the WebAssembly entry point for pdfcpu.
//
// It exports every major pdfcpu API function via syscall/js so that
// JavaScript / TypeScript callers can:
//   - validate, optimize, merge, split, trim, collect, rotate, extract pages
//   - encrypt, decrypt, watermark, bookmark, annotate, form operations
//   - n-up, booklet, zoom, resize, crop, box management
//   - attach, keyword, property, layout, page mode, viewer preferences
//   - extract images, fonts, pages, content, metadata
//
// Build:
//
//	GOOS=js GOARCH=wasm go build -tags js -trimpath -o dist/pdfcpu.wasm .
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"syscall/js"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// ---------------------------------------------------------------------------
// Configuration helpers
// ---------------------------------------------------------------------------

// wasmConfig is deserialized from a JSON Uint8Array passed from JS.
type wasmConfig struct {
	ValidationMode   string `json:"validationMode,omitempty"`
	Unit             string `json:"unit,omitempty"`
	OwnerPW          string `json:"ownerPW,omitempty"`
	UserPW           string `json:"userPW,omitempty"`
	EncryptKeyLength int    `json:"encryptKeyLength,omitempty"`
	Permissions      int    `json:"permissions,omitempty"`
}

func toConf(cfg *wasmConfig) *model.Configuration {
	conf := model.NewDefaultConfiguration()
	if cfg == nil {
		return conf
	}
	switch cfg.ValidationMode {
	case "strict":
		conf.ValidationMode = model.ValidationStrict
	case "relaxed":
		conf.ValidationMode = model.ValidationRelaxed
	}
	if cfg.Unit != "" {
		conf.SetUnit(cfg.Unit)
	}
	if cfg.OwnerPW != "" {
		conf.OwnerPW = cfg.OwnerPW
	}
	if cfg.UserPW != "" {
		conf.UserPW = cfg.UserPW
	}
	if cfg.Permissions != 0 {
		conf.Permissions = model.PermissionFlags(cfg.Permissions)
	}
	if cfg.EncryptKeyLength > 0 {
		conf.EncryptKeyLength = cfg.EncryptKeyLength
	}
	return conf
}

// ---------------------------------------------------------------------------
// JS value conversion helpers
// ---------------------------------------------------------------------------

// bytesFromJS copies a JS Uint8Array (or ArrayBuffer) into a Go []byte.
func bytesFromJS(v js.Value) ([]byte, bool) {
	if !v.Truthy() || v.IsUndefined() || v.IsNull() {
		return nil, false
	}
	l := v.Get("length").Int()
	if l == 0 {
		return nil, false
	}
	b := make([]byte, l)
	if n := js.CopyBytesToGo(b, v); n > 0 {
		return b[:n], true
	}
	return nil, false
}

// bytesToJS wraps a Go []byte as a JS Uint8Array.
func bytesToJS(b []byte) js.Value {
	u := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(u, b)
	return u
}

// stringsFromJS converts a JS array of strings to a Go []string.
func stringsFromJS(v js.Value) []string {
	if !v.Truthy() || v.IsUndefined() || v.IsNull() {
		return nil
	}
	n := v.Get("length").Int()
	s := make([]string, 0, n)
	for i := range n {
		s = append(s, v.Index(i).String())
	}
	return s
}

// intsFromJS converts a JS array of numbers to a Go []int.
func intsFromJS(v js.Value) []int {
	if !v.Truthy() || v.IsUndefined() || v.IsNull() {
		return nil
	}
	n := v.Get("length").Int()
	s := make([]int, 0, n)
	for i := range n {
		s = append(s, v.Index(i).Int())
	}
	return s
}

// confFromJS reads an optional JSON-encoded config Uint8Array from JS.
func confFromJS(v js.Value) *wasmConfig {
	if !v.Truthy() || v.IsUndefined() || v.IsNull() {
		return nil
	}
	b, ok := bytesFromJS(v)
	if !ok {
		return nil
	}
	var cfg wasmConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil
	}
	return &cfg
}

// ---------------------------------------------------------------------------
// Result helpers
// ---------------------------------------------------------------------------

// makeResult returns a JS object {ok: bool, data?: ..., error?: string}.
func makeResult(data any, err error) js.Value {
	obj := js.Global().Get("Object").New()
	if err != nil {
		obj.Set("ok", false)
		obj.Set("error", err.Error())
		return obj
	}
	obj.Set("ok", true)
	switch v := data.(type) {
	case []byte:
		obj.Set("data", bytesToJS(v))
	case string:
		obj.Set("data", v)
	default:
		obj.Set("data", js.Undefined())
	}
	return obj
}

// arg returns the i-th argument or an undefined value.
func arg(args []js.Value, i int) js.Value {
	if i < len(args) {
		return args[i]
	}
	return js.Undefined()
}

// ---------------------------------------------------------------------------
// JSON parsing helpers for complex model types
// ---------------------------------------------------------------------------

type wasmBox struct {
	Rect []float64 `json:"rect"` // [x1, y1, x2, y2]
}

func boxFromJSON(data []byte) (*model.Box, error) {
	var wb wasmBox
	if err := json.Unmarshal(data, &wb); err != nil {
		return nil, err
	}
	if len(wb.Rect) != 4 {
		return nil, fmt.Errorf("box rect requires exactly 4 coordinates, got %d", len(wb.Rect))
	}
	return &model.Box{
		Rect: types.NewRectangle(wb.Rect[0], wb.Rect[1], wb.Rect[2], wb.Rect[3]),
	}, nil
}

type wasmWatermark struct {
	Text     string  `json:"text"`
	FontSize int     `json:"fontSize"`
	Opacity  float64 `json:"opacity"`
	OnTop    bool    `json:"onTop"`
	Rotation float64 `json:"rotation"`
	Scale    float64 `json:"scale"`
}

func watermarkFromJSON(data []byte) (*model.Watermark, error) {
	var ww wasmWatermark
	if err := json.Unmarshal(data, &ww); err != nil {
		return nil, err
	}
	wm := &model.Watermark{
		Mode:       model.WMText,
		OnTop:      ww.OnTop,
		TextString: ww.Text,
		FontName:   "Helvetica",
		FontSize:   24,
		Opacity:    ww.Opacity,
		Rotation:   ww.Rotation,
		Scale:      ww.Scale,
	}
	if ww.FontSize > 0 {
		wm.FontSize = ww.FontSize
	}
	return wm, nil
}

type wasmZoom struct {
	Factor float64 `json:"factor"`
}

func zoomFromJSON(data []byte) (*model.Zoom, error) {
	var wz wasmZoom
	if err := json.Unmarshal(data, &wz); err != nil {
		return nil, err
	}
	return &model.Zoom{Factor: wz.Factor}, nil
}

type wasmResize struct {
	Scale    float64 `json:"scale"`
	PageSize string  `json:"pageSize"`
}

func resizeFromJSON(data []byte) (*model.Resize, error) {
	var wr wasmResize
	if err := json.Unmarshal(data, &wr); err != nil {
		return nil, err
	}
	r := &model.Resize{
		Scale:    wr.Scale,
		PageSize: wr.PageSize,
	}
	if r.PageSize == "" {
		r.PageSize = "A4"
	}
	return r, nil
}

type wasmNUp struct {
	PageSize   string  `json:"pageSize"`
	PageWidth  float64 `json:"pageWidth"`
	PageHeight float64 `json:"pageHeight"`
	GridRows   int     `json:"gridRows"`
	GridCols   int     `json:"gridCols"`
	Margin     float64 `json:"margin"`
	Border     bool    `json:"border"`
}

func nupFromJSON(data []byte) (*model.NUp, error) {
	var wn wasmNUp
	if err := json.Unmarshal(data, &wn); err != nil {
		return nil, err
	}
	nup := model.DefaultNUpConfig()
	if wn.PageSize != "" {
		nup.PageSize = wn.PageSize
	}
	if wn.PageWidth > 0 && wn.PageHeight > 0 {
		nup.PageDim = &types.Dim{Width: wn.PageWidth, Height: wn.PageHeight}
	}
	if wn.GridRows > 0 && wn.GridCols > 0 {
		nup.Grid = &types.Dim{Width: float64(wn.GridCols), Height: float64(wn.GridRows)}
	}
	if wn.Margin > 0 {
		nup.Margin = wn.Margin
	}
	nup.Border = wn.Border
	return nup, nil
}

// ---------------------------------------------------------------------------
// Document-level operations
// ---------------------------------------------------------------------------

func validateWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("validate: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))
	if err := api.Validate(bytes.NewReader(data), toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult("validation ok", nil)
}

func optimizeWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("optimize: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))
	var buf bytes.Buffer
	if err := api.Optimize(bytes.NewReader(data), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func infoWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("info: missing or empty input buffer"))
	}
	selectedPages := stringsFromJS(arg(args, 1))
	fonts := arg(args, 2).Truthy()
	conf := confFromJS(arg(args, 3))

	info, err := api.PDFInfo(bytes.NewReader(data), "document.pdf", selectedPages, fonts, toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(info)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func pageCountWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("pageCount: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))
	count, err := api.PageCount(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(strconv.Itoa(count), nil)
}

func pageDimsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("pageDims: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))
	dims, err := api.PageDims(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(dims)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

// ---------------------------------------------------------------------------
// Page operations
// ---------------------------------------------------------------------------

func trimWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("trim: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.Trim(bytes.NewReader(data), &buf, pages, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func collectWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("collect: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.Collect(bytes.NewReader(data), &buf, pages, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func rotateWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("rotate: missing or empty input buffer"))
	}
	rotation := 90
	if r := arg(args, 1); !r.IsUndefined() {
		rotation = r.Int()
	}
	pages := stringsFromJS(arg(args, 2))
	conf := confFromJS(arg(args, 3))

	var buf bytes.Buffer
	if err := api.Rotate(bytes.NewReader(data), &buf, rotation, pages, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func insertPagesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("insertPages: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	before := false
	if b := arg(args, 2); !b.IsUndefined() {
		before = b.Truthy()
	}
	conf := confFromJS(arg(args, 3))

	var buf bytes.Buffer
	if err := api.InsertPages(bytes.NewReader(data), &buf, pages, before, nil, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func removePagesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removePages: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.RemovePages(bytes.NewReader(data), &buf, pages, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func cropWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("crop: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	boxJSON := arg(args, 2)
	conf := confFromJS(arg(args, 3))

	boxBytes, ok := bytesFromJS(boxJSON)
	if !ok {
		return makeResult(nil, wasmError("crop: missing box JSON"))
	}
	b, err := boxFromJSON(boxBytes)
	if err != nil {
		return makeResult(nil, wasmError("crop: invalid box JSON: "+err.Error()))
	}

	var buf bytes.Buffer
	if err := api.Crop(bytes.NewReader(data), &buf, pages, b, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func addBoxesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("addBoxes: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	pbJSON := arg(args, 2)
	conf := confFromJS(arg(args, 3))

	pbBytes, ok := bytesFromJS(pbJSON)
	if !ok {
		return makeResult(nil, wasmError("addBoxes: missing pageBoundaries JSON"))
	}
	var pb model.PageBoundaries
	if err := json.Unmarshal(pbBytes, &pb); err != nil {
		return makeResult(nil, wasmError("addBoxes: invalid JSON: "+err.Error()))
	}

	var buf bytes.Buffer
	if err := api.AddBoxes(bytes.NewReader(data), &buf, pages, &pb, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func removeBoxesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removeBoxes: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	pbJSON := arg(args, 2)
	conf := confFromJS(arg(args, 3))

	pbBytes, ok := bytesFromJS(pbJSON)
	if !ok {
		return makeResult(nil, wasmError("removeBoxes: missing pageBoundaries JSON"))
	}
	var pb model.PageBoundaries
	if err := json.Unmarshal(pbBytes, &pb); err != nil {
		return makeResult(nil, wasmError("removeBoxes: invalid JSON: "+err.Error()))
	}

	var buf bytes.Buffer
	if err := api.RemoveBoxes(bytes.NewReader(data), &buf, pages, &pb, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Security
// ---------------------------------------------------------------------------

func encryptWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("encrypt: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	var buf bytes.Buffer
	if err := api.Encrypt(bytes.NewReader(data), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func decryptWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("decrypt: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	var buf bytes.Buffer
	if err := api.Decrypt(bytes.NewReader(data), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func changeUserPasswordWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("changeUserPassword: missing or empty input buffer"))
	}
	pwOld := arg(args, 1).String()
	pwNew := arg(args, 2).String()
	conf := confFromJS(arg(args, 3))

	var buf bytes.Buffer
	if err := api.ChangeUserPassword(bytes.NewReader(data), &buf, pwOld, pwNew, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func changeOwnerPasswordWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("changeOwnerPassword: missing or empty input buffer"))
	}
	pwOld := arg(args, 1).String()
	pwNew := arg(args, 2).String()
	conf := confFromJS(arg(args, 3))

	var buf bytes.Buffer
	if err := api.ChangeOwnerPassword(bytes.NewReader(data), &buf, pwOld, pwNew, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func getPermissionsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("getPermissions: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	p, err := api.GetPermissions(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	if p == nil {
		return makeResult("null", nil)
	}
	return makeResult(fmt.Sprintf("%d", *p), nil)
}

func setPermissionsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("setPermissions: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	var buf bytes.Buffer
	if err := api.SetPermissions(bytes.NewReader(data), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func permissionsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("permissions: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	p, err := api.Permissions(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(strconv.Itoa(p), nil)
}

// ---------------------------------------------------------------------------
// Content
// ---------------------------------------------------------------------------

func addWatermarksWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("addWatermarks: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	wmJSON := arg(args, 2)
	conf := confFromJS(arg(args, 3))

	wmBytes, ok := bytesFromJS(wmJSON)
	if !ok {
		return makeResult(nil, wasmError("addWatermarks: missing watermark JSON"))
	}
	wm, err := watermarkFromJSON(wmBytes)
	if err != nil {
		return makeResult(nil, err)
	}

	var buf bytes.Buffer
	if err := api.AddWatermarks(bytes.NewReader(data), &buf, pages, wm, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func removeWatermarksWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removeWatermarks: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.RemoveWatermarks(bytes.NewReader(data), &buf, pages, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func hasWatermarksWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("hasWatermarks: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	ok, err := api.HasWatermarks(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(fmt.Sprintf("%t", ok), nil)
}

func listAnnotationsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("listAnnotations: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	annots, err := api.Annotations(bytes.NewReader(data), pages, toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(annots)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func removeAnnotationsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removeAnnotations: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	idsAndTypes := stringsFromJS(arg(args, 2))
	objNrs := intsFromJS(arg(args, 3))
	conf := confFromJS(arg(args, 4))

	var buf bytes.Buffer
	if err := api.RemoveAnnotations(bytes.NewReader(data), &buf, pages, idsAndTypes, objNrs, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Bookmarks
// ---------------------------------------------------------------------------

func listBookmarksWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("listBookmarks: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	bms, err := api.Bookmarks(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(bms)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func addBookmarksWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("addBookmarks: missing or empty input buffer"))
	}
	bmsJSON := arg(args, 1)
	replace := arg(args, 2).Truthy()
	conf := confFromJS(arg(args, 3))

	bmsBytes, ok := bytesFromJS(bmsJSON)
	if !ok {
		return makeResult(nil, wasmError("addBookmarks: missing bookmarks JSON"))
	}
	var bms []pdfcpu.Bookmark
	if err := json.Unmarshal(bmsBytes, &bms); err != nil {
		return makeResult(nil, wasmError("addBookmarks: invalid JSON: "+err.Error()))
	}

	var buf bytes.Buffer
	if err := api.AddBookmarks(bytes.NewReader(data), &buf, bms, replace, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func removeBookmarksWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removeBookmarks: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	var buf bytes.Buffer
	if err := api.RemoveBookmarks(bytes.NewReader(data), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func exportBookmarksWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("exportBookmarks: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	var buf bytes.Buffer
	if err := api.ExportBookmarksJSON(bytes.NewReader(data), &buf, "document.pdf", toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.String(), nil)
}

func importBookmarksWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("importBookmarks: missing or empty input buffer"))
	}
	bmsJSON := arg(args, 1)
	replace := arg(args, 2).Truthy()
	conf := confFromJS(arg(args, 3))

	bmsBytes, ok := bytesFromJS(bmsJSON)
	if !ok {
		return makeResult(nil, wasmError("importBookmarks: missing bookmarks JSON"))
	}

	var buf bytes.Buffer
	if err := api.ImportBookmarks(bytes.NewReader(data), bytes.NewReader(bmsBytes), &buf, replace, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Attachments
// ---------------------------------------------------------------------------

func listAttachmentsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("listAttachments: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	attachments, err := api.Attachments(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	type attResult struct {
		ID       string `json:"id"`
		FileName string `json:"fileName"`
		Desc     string `json:"desc"`
	}
	result := make([]attResult, 0, len(attachments))
	for _, a := range attachments {
		result = append(result, attResult{ID: a.ID, FileName: a.FileName, Desc: a.Desc})
	}
	bb, err := json.Marshal(result)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func addAttachmentsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("addAttachments: missing or empty input buffer"))
	}
	fileNames := stringsFromJS(arg(args, 1))
	asPortfolio := arg(args, 2).Truthy()
	conf := confFromJS(arg(args, 3))

	var buf bytes.Buffer
	if err := api.AddAttachments(bytes.NewReader(data), &buf, fileNames, asPortfolio, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func removeAttachmentsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removeAttachments: missing or empty input buffer"))
	}
	fileNames := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.RemoveAttachments(bytes.NewReader(data), &buf, fileNames, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Form fields
// ---------------------------------------------------------------------------

func listFormFieldsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("listFormFields: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	fields, err := api.FormFields(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(fields)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func removeFormFieldsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removeFormFields: missing or empty input buffer"))
	}
	fieldIDsOrNames := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.RemoveFormFields(bytes.NewReader(data), &buf, fieldIDsOrNames, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func lockFormFieldsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("lockFormFields: missing or empty input buffer"))
	}
	fieldIDsOrNames := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.LockFormFields(bytes.NewReader(data), &buf, fieldIDsOrNames, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func unlockFormFieldsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("unlockFormFields: missing or empty input buffer"))
	}
	fieldIDsOrNames := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.UnlockFormFields(bytes.NewReader(data), &buf, fieldIDsOrNames, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func resetFormFieldsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("resetFormFields: missing or empty input buffer"))
	}
	fieldIDsOrNames := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.ResetFormFields(bytes.NewReader(data), &buf, fieldIDsOrNames, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Keywords & Properties
// ---------------------------------------------------------------------------

func listKeywordsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("listKeywords: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	keywords, err := api.Keywords(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(keywords)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func addKeywordsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("addKeywords: missing or empty input buffer"))
	}
	keywords := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.AddKeywords(bytes.NewReader(data), &buf, keywords, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func removeKeywordsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removeKeywords: missing or empty input buffer"))
	}
	keywords := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.RemoveKeywords(bytes.NewReader(data), &buf, keywords, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func listPropertiesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("listProperties: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	props, err := api.Properties(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(props)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func addPropertiesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("addProperties: missing or empty input buffer"))
	}
	propsJSON := arg(args, 1)
	conf := confFromJS(arg(args, 2))

	propsBytes, ok := bytesFromJS(propsJSON)
	if !ok {
		return makeResult(nil, wasmError("addProperties: missing properties JSON"))
	}
	var props map[string]string
	if err := json.Unmarshal(propsBytes, &props); err != nil {
		return makeResult(nil, wasmError("addProperties: invalid JSON: "+err.Error()))
	}

	var buf bytes.Buffer
	if err := api.AddProperties(bytes.NewReader(data), &buf, props, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func removePropertiesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removeProperties: missing or empty input buffer"))
	}
	propNames := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.RemoveProperties(bytes.NewReader(data), &buf, propNames, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Layout/Metadata
// ---------------------------------------------------------------------------

func getPageLayoutWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("getPageLayout: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	pl, err := api.PageLayout(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(pl)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func setPageLayoutWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("setPageLayout: missing or empty input buffer"))
	}
	layoutStr := arg(args, 1).String()
	conf := confFromJS(arg(args, 2))

	pl := model.PageLayoutFor(layoutStr)
	if pl == nil {
		return makeResult(nil, wasmError("setPageLayout: invalid layout: "+layoutStr))
	}

	var buf bytes.Buffer
	if err := api.SetPageLayout(bytes.NewReader(data), &buf, *pl, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func resetPageLayoutWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("resetPageLayout: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	var buf bytes.Buffer
	if err := api.ResetPageLayout(bytes.NewReader(data), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func getPageModeWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("getPageMode: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	pm, err := api.PageMode(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(pm)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func setPageModeWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("setPageMode: missing or empty input buffer"))
	}
	modeStr := arg(args, 1).String()
	conf := confFromJS(arg(args, 2))

	pm := model.PageModeFor(modeStr)
	if pm == nil {
		return makeResult(nil, wasmError("setPageMode: invalid mode: "+modeStr))
	}

	var buf bytes.Buffer
	if err := api.SetPageMode(bytes.NewReader(data), &buf, *pm, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func resetPageModeWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("resetPageMode: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	var buf bytes.Buffer
	if err := api.ResetPageMode(bytes.NewReader(data), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func getViewerPreferencesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("getViewerPreferences: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	vp, _, err := api.ViewerPreferences(bytes.NewReader(data), toConf(conf))
	if err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(vp)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func setViewerPreferencesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("setViewerPreferences: missing or empty input buffer"))
	}
	prefsJSON := arg(args, 1)
	conf := confFromJS(arg(args, 2))

	prefsBytes, ok := bytesFromJS(prefsJSON)
	if !ok {
		return makeResult(nil, wasmError("setViewerPreferences: missing preferences JSON"))
	}

	var buf bytes.Buffer
	if err := api.SetViewerPreferencesFromJSONBytes(bytes.NewReader(data), &buf, prefsBytes, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func resetViewerPreferencesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("resetViewerPreferences: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	var buf bytes.Buffer
	if err := api.ResetViewerPreferences(bytes.NewReader(data), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Merge / Split
// ---------------------------------------------------------------------------

func mergeWASM(this js.Value, args []js.Value) any {
	inputs := arg(args, 0)
	if !inputs.Truthy() || inputs.IsUndefined() || inputs.IsNull() {
		return makeResult(nil, wasmError("merge: missing input array"))
	}
	n := inputs.Get("length").Int()
	if n < 2 {
		return makeResult(nil, wasmError("merge: need at least 2 PDF buffers"))
	}
	readers := make([]io.ReadSeeker, 0, n)
	for i := range n {
		b, ok := bytesFromJS(inputs.Index(i))
		if !ok {
			return makeResult(nil, wasmError("merge: empty PDF at index " + itoa(i)))
		}
		readers = append(readers, bytes.NewReader(b))
	}
	dividerPage := arg(args, 1).Truthy()
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.MergeRaw(readers, &buf, dividerPage, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func mergeZipWASM(this js.Value, args []js.Value) any {
	data1, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("mergeZip: missing first input buffer"))
	}
	data2, ok := bytesFromJS(arg(args, 1))
	if !ok {
		return makeResult(nil, wasmError("mergeZip: missing second input buffer"))
	}
	conf := confFromJS(arg(args, 2))

	var buf bytes.Buffer
	if err := api.MergeCreateZip(bytes.NewReader(data1), bytes.NewReader(data2), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Layout operations (complex params via JSON)
// ---------------------------------------------------------------------------

func nupWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("nup: missing or empty input buffer"))
	}
	imgFileNames := stringsFromJS(arg(args, 1))
	pages := stringsFromJS(arg(args, 2))
	nupJSON := arg(args, 3)
	conf := confFromJS(arg(args, 4))

	nupBytes, ok := bytesFromJS(nupJSON)
	if !ok {
		return makeResult(nil, wasmError("nup: missing nup JSON"))
	}
	nup, err := nupFromJSON(nupBytes)
	if err != nil {
		return makeResult(nil, err)
	}

	var buf bytes.Buffer
	if err := api.NUp(bytes.NewReader(data), &buf, imgFileNames, pages, nup, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func bookletWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("booklet: missing or empty input buffer"))
	}
	imgFileNames := stringsFromJS(arg(args, 1))
	pages := stringsFromJS(arg(args, 2))
	nupJSON := arg(args, 3)
	conf := confFromJS(arg(args, 4))

	nupBytes, ok := bytesFromJS(nupJSON)
	if !ok {
		return makeResult(nil, wasmError("booklet: missing nup JSON"))
	}
	nup, err := nupFromJSON(nupBytes)
	if err != nil {
		return makeResult(nil, err)
	}

	var buf bytes.Buffer
	if err := api.Booklet(bytes.NewReader(data), &buf, imgFileNames, pages, nup, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func zoomWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("zoom: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	zoomJSON := arg(args, 2)
	conf := confFromJS(arg(args, 3))

	zoomBytes, ok := bytesFromJS(zoomJSON)
	if !ok {
		return makeResult(nil, wasmError("zoom: missing zoom JSON"))
	}
	z, err := zoomFromJSON(zoomBytes)
	if err != nil {
		return makeResult(nil, err)
	}

	var buf bytes.Buffer
	if err := api.Zoom(bytes.NewReader(data), &buf, pages, z, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

func resizeWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("resize: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	resizeJSON := arg(args, 2)
	conf := confFromJS(arg(args, 3))

	resizeBytes, ok := bytesFromJS(resizeJSON)
	if !ok {
		return makeResult(nil, wasmError("resize: missing resize JSON"))
	}
	r, err := resizeFromJSON(resizeBytes)
	if err != nil {
		return makeResult(nil, err)
	}

	var buf bytes.Buffer
	if err := api.Resize(bytes.NewReader(data), &buf, pages, r, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Signatures
// ---------------------------------------------------------------------------

func removeSignaturesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("removeSignatures: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	var buf bytes.Buffer
	if err := api.RemoveSignatures(bytes.NewReader(data), &buf, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	return makeResult(buf.Bytes(), nil)
}

// ---------------------------------------------------------------------------
// Extraction (callback-based — collect into memory)
// ---------------------------------------------------------------------------

func extractImagesWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("extractImages: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	type imgResult struct {
		Name     string `json:"name"`
		FileType string `json:"fileType"`
		PageNr   int    `json:"pageNr"`
		ObjNr    int    `json:"objNr"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Size     int64  `json:"size"`
		Data     string `json:"data,omitempty"`
	}

	var results []imgResult

	digest := func(img model.Image, singleImgPerPage bool, maxPageDigits int) error {
		var imgData []byte
		if img.Reader != nil {
			var b bytes.Buffer
			if _, err := io.Copy(&b, img.Reader); err != nil {
				return err
			}
			imgData = b.Bytes()
		}
		r := imgResult{
			Name:     img.Name,
			FileType: img.FileType,
			PageNr:   img.PageNr,
			ObjNr:    img.ObjNr,
			Width:    img.Width,
			Height:   img.Height,
			Size:     img.Size,
		}
		if len(imgData) > 0 {
			r.Data = base64.StdEncoding.EncodeToString(imgData)
		}
		results = append(results, r)
		return nil
	}

	if err := api.ExtractImages(bytes.NewReader(data), pages, digest, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(results)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func extractFontsWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("extractFonts: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	type fontResult struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Data string `json:"data,omitempty"`
	}

	var results []fontResult

	digest := func(font pdfcpu.Font) error {
		var fontData []byte
		if font.Reader != nil {
			var b bytes.Buffer
			if _, err := io.Copy(&b, font.Reader); err != nil {
				return err
			}
			fontData = b.Bytes()
		}
		r := fontResult{
			Name: font.Name,
			Type: font.Type,
		}
		if len(fontData) > 0 {
			r.Data = base64.StdEncoding.EncodeToString(fontData)
		}
		results = append(results, r)
		return nil
	}

	if err := api.ExtractFonts(bytes.NewReader(data), pages, digest, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(results)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func extractPagesReaderWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("extractPagesReader: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	type pageResult struct {
		PageNr int    `json:"pageNr"`
		Data   []byte `json:"-"`
	}

	var results []pageResult

	digest := func(rd io.Reader, pageNr int) error {
		var b bytes.Buffer
		if _, err := io.Copy(&b, rd); err != nil {
			return err
		}
		results = append(results, pageResult{PageNr: pageNr, Data: b.Bytes()})
		return nil
	}

	if err := api.ExtractPages(bytes.NewReader(data), pages, digest, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}

	// Return as array of Uint8Arrays
	arr := js.Global().Get("Array").New(len(results))
	for i, r := range results {
		obj := js.Global().Get("Object").New()
		obj.Set("pageNr", r.PageNr)
		obj.Set("data", bytesToJS(r.Data))
		arr.SetIndex(i, obj)
	}
	resultObj := js.Global().Get("Object").New()
	resultObj.Set("ok", true)
	resultObj.Set("data", arr)
	return resultObj
}

func extractContentWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("extractContent: missing or empty input buffer"))
	}
	pages := stringsFromJS(arg(args, 1))
	conf := confFromJS(arg(args, 2))

	type contentResult struct {
		PageNr  int    `json:"pageNr"`
		Content string `json:"content"`
	}

	var results []contentResult

	digest := func(rd io.Reader, pageNr int) error {
		var b bytes.Buffer
		if _, err := io.Copy(&b, rd); err != nil {
			return err
		}
		results = append(results, contentResult{PageNr: pageNr, Content: b.String()})
		return nil
	}

	if err := api.ExtractContent(bytes.NewReader(data), pages, digest, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(results)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

func extractMetadataWASM(this js.Value, args []js.Value) any {
	data, ok := bytesFromJS(arg(args, 0))
	if !ok {
		return makeResult(nil, wasmError("extractMetadata: missing or empty input buffer"))
	}
	conf := confFromJS(arg(args, 1))

	type mdResult struct {
		ObjNr       int    `json:"objNr"`
		ParentObjNr int    `json:"parentObjNr"`
		ParentType  string `json:"parentType"`
		Data        string `json:"data,omitempty"`
	}

	var results []mdResult

	digest := func(md pdfcpu.Metadata) error {
		var b bytes.Buffer
		if _, err := io.Copy(&b, md.Reader); err != nil {
			return err
		}
		results = append(results, mdResult{
			ObjNr:       md.ObjNr,
			ParentObjNr: md.ParentObjNr,
			ParentType:  md.ParentType,
			Data:        b.String(),
		})
		return nil
	}

	if err := api.ExtractMetadata(bytes.NewReader(data), digest, toConf(conf)); err != nil {
		return makeResult(nil, err)
	}
	bb, err := json.Marshal(results)
	if err != nil {
		return makeResult(nil, err)
	}
	return makeResult(string(bb), nil)
}

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

func registerFunctions() {
	// Document-level
	js.Global().Set("pdfcpu_validate", js.FuncOf(validateWASM))
	js.Global().Set("pdfcpu_optimize", js.FuncOf(optimizeWASM))
	js.Global().Set("pdfcpu_info", js.FuncOf(infoWASM))
	js.Global().Set("pdfcpu_pageCount", js.FuncOf(pageCountWASM))
	js.Global().Set("pdfcpu_pageDims", js.FuncOf(pageDimsWASM))

	// Page operations
	js.Global().Set("pdfcpu_trim", js.FuncOf(trimWASM))
	js.Global().Set("pdfcpu_collect", js.FuncOf(collectWASM))
	js.Global().Set("pdfcpu_rotate", js.FuncOf(rotateWASM))
	js.Global().Set("pdfcpu_insertPages", js.FuncOf(insertPagesWASM))
	js.Global().Set("pdfcpu_removePages", js.FuncOf(removePagesWASM))
	js.Global().Set("pdfcpu_crop", js.FuncOf(cropWASM))
	js.Global().Set("pdfcpu_addBoxes", js.FuncOf(addBoxesWASM))
	js.Global().Set("pdfcpu_removeBoxes", js.FuncOf(removeBoxesWASM))

	// Security
	js.Global().Set("pdfcpu_encrypt", js.FuncOf(encryptWASM))
	js.Global().Set("pdfcpu_decrypt", js.FuncOf(decryptWASM))
	js.Global().Set("pdfcpu_changeUserPassword", js.FuncOf(changeUserPasswordWASM))
	js.Global().Set("pdfcpu_changeOwnerPassword", js.FuncOf(changeOwnerPasswordWASM))
	js.Global().Set("pdfcpu_getPermissions", js.FuncOf(getPermissionsWASM))
	js.Global().Set("pdfcpu_setPermissions", js.FuncOf(setPermissionsWASM))
	js.Global().Set("pdfcpu_permissions", js.FuncOf(permissionsWASM))

	// Content
	js.Global().Set("pdfcpu_addWatermarks", js.FuncOf(addWatermarksWASM))
	js.Global().Set("pdfcpu_removeWatermarks", js.FuncOf(removeWatermarksWASM))
	js.Global().Set("pdfcpu_hasWatermarks", js.FuncOf(hasWatermarksWASM))
	js.Global().Set("pdfcpu_listAnnotations", js.FuncOf(listAnnotationsWASM))
	js.Global().Set("pdfcpu_removeAnnotations", js.FuncOf(removeAnnotationsWASM))

	// Bookmarks
	js.Global().Set("pdfcpu_listBookmarks", js.FuncOf(listBookmarksWASM))
	js.Global().Set("pdfcpu_addBookmarks", js.FuncOf(addBookmarksWASM))
	js.Global().Set("pdfcpu_removeBookmarks", js.FuncOf(removeBookmarksWASM))
	js.Global().Set("pdfcpu_exportBookmarks", js.FuncOf(exportBookmarksWASM))
	js.Global().Set("pdfcpu_importBookmarks", js.FuncOf(importBookmarksWASM))

	// Attachments
	js.Global().Set("pdfcpu_listAttachments", js.FuncOf(listAttachmentsWASM))
	js.Global().Set("pdfcpu_addAttachments", js.FuncOf(addAttachmentsWASM))
	js.Global().Set("pdfcpu_removeAttachments", js.FuncOf(removeAttachmentsWASM))

	// Form fields
	js.Global().Set("pdfcpu_listFormFields", js.FuncOf(listFormFieldsWASM))
	js.Global().Set("pdfcpu_removeFormFields", js.FuncOf(removeFormFieldsWASM))
	js.Global().Set("pdfcpu_lockFormFields", js.FuncOf(lockFormFieldsWASM))
	js.Global().Set("pdfcpu_unlockFormFields", js.FuncOf(unlockFormFieldsWASM))
	js.Global().Set("pdfcpu_resetFormFields", js.FuncOf(resetFormFieldsWASM))

	// Keywords & Properties
	js.Global().Set("pdfcpu_listKeywords", js.FuncOf(listKeywordsWASM))
	js.Global().Set("pdfcpu_addKeywords", js.FuncOf(addKeywordsWASM))
	js.Global().Set("pdfcpu_removeKeywords", js.FuncOf(removeKeywordsWASM))
	js.Global().Set("pdfcpu_listProperties", js.FuncOf(listPropertiesWASM))
	js.Global().Set("pdfcpu_addProperties", js.FuncOf(addPropertiesWASM))
	js.Global().Set("pdfcpu_removeProperties", js.FuncOf(removePropertiesWASM))

	// Layout/Metadata
	js.Global().Set("pdfcpu_getPageLayout", js.FuncOf(getPageLayoutWASM))
	js.Global().Set("pdfcpu_setPageLayout", js.FuncOf(setPageLayoutWASM))
	js.Global().Set("pdfcpu_resetPageLayout", js.FuncOf(resetPageLayoutWASM))
	js.Global().Set("pdfcpu_getPageMode", js.FuncOf(getPageModeWASM))
	js.Global().Set("pdfcpu_setPageMode", js.FuncOf(setPageModeWASM))
	js.Global().Set("pdfcpu_resetPageMode", js.FuncOf(resetPageModeWASM))
	js.Global().Set("pdfcpu_getViewerPreferences", js.FuncOf(getViewerPreferencesWASM))
	js.Global().Set("pdfcpu_setViewerPreferences", js.FuncOf(setViewerPreferencesWASM))
	js.Global().Set("pdfcpu_resetViewerPreferences", js.FuncOf(resetViewerPreferencesWASM))

	// Merge / Split
	js.Global().Set("pdfcpu_merge", js.FuncOf(mergeWASM))
	js.Global().Set("pdfcpu_mergeZip", js.FuncOf(mergeZipWASM))

	// Layout operations
	js.Global().Set("pdfcpu_nup", js.FuncOf(nupWASM))
	js.Global().Set("pdfcpu_booklet", js.FuncOf(bookletWASM))
	js.Global().Set("pdfcpu_zoom", js.FuncOf(zoomWASM))
	js.Global().Set("pdfcpu_resize", js.FuncOf(resizeWASM))

	// Signatures
	js.Global().Set("pdfcpu_removeSignatures", js.FuncOf(removeSignaturesWASM))

	// Extraction
	js.Global().Set("pdfcpu_extractImages", js.FuncOf(extractImagesWASM))
	js.Global().Set("pdfcpu_extractFonts", js.FuncOf(extractFontsWASM))
	js.Global().Set("pdfcpu_extractPagesReader", js.FuncOf(extractPagesReaderWASM))
	js.Global().Set("pdfcpu_extractContent", js.FuncOf(extractContentWASM))
	js.Global().Set("pdfcpu_extractMetadata", js.FuncOf(extractMetadataWASM))
}

func main() {
	// Disable config dir — no filesystem in browser WASM.
	api.DisableConfigDir()

	registerFunctions()

	// Signal the module is ready.
	js.Global().Set("pdfcpu_ready", true)

	// Keep the Go runtime alive so JS can call exported functions.
	select {}
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

type wasmError string

func (e wasmError) Error() string { return string(e) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
