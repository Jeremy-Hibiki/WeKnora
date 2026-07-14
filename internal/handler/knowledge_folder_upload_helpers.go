package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

// parseBoolSafe parses a bool without panicking on bad input.
func parseBoolSafe(s string) (bool, error) {
	return strconv.ParseBool(strings.TrimSpace(s))
}

// parseJSONSafe unmarshals JSON without panicking on bad input.
func parseJSONSafe(raw string, dst interface{}) error {
	return json.Unmarshal([]byte(raw), dst)
}

// itoa is a thin strconv.Itoa wrapper kept for readability at call sites.
func itoa(i int) string { return strconv.Itoa(i) }

// isDuplicateKnowledgeError reports whether err is the typed duplicate-file
// error returned by CreateKnowledgeFromFile (used to apply the skip policy).
func isDuplicateKnowledgeError(err error) bool {
	var dupErr *types.DuplicateKnowledgeError
	return errors.As(err, &dupErr)
}

// isZipArchive reports whether the filename looks like a zip archive.
func isZipArchive(name string) bool {
	return strings.EqualFold(strings.ToLower(filepath.Ext(name)), ".zip")
}

// maxDecompressedZipBytes caps the total bytes extracted from a single zip
// upload to protect against zip bombs and runaway memory usage.
const maxDecompressedZipBytes = 1 << 30 // 1 GiB

// extractZipToEntries opens the uploaded zip and returns one entry per regular
// file, re-packaged as a real *multipart.FileHeader (so the downstream service
// flow — calculateFileHash + SaveFile — works unchanged). Each entry is fully
// buffered in memory; this is acceptable because individual files are already
// capped by MAX_FILE_SIZE_MB and the whole upload by maxFolderUploadFiles.
//
// The total decompressed bytes are capped at maxDecompressedZipBytes to prevent
// zip-bomb exhaustion. Zip Slip (entries that escape via ../) and macOS noise
// (__MACOSX, ._* ) are filtered out. The returned relPaths are forward-slashed
// relative paths.
func extractZipToEntries(zipHeader *multipart.FileHeader) ([]*multipart.FileHeader, []string, error) {
	src, err := zipHeader.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("open zip upload: %w", err)
	}
	defer src.Close()

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, src); err != nil {
		return nil, nil, fmt.Errorf("read zip upload: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		return nil, nil, fmt.Errorf("open zip reader: %w", err)
	}

	var (
		headers   []*multipart.FileHeader
		relPaths  []string
		skipCount int
		extracted int64
	)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel := filepath.ToSlash(f.Name)
		// Drop macOS metadata and hidden junk that would pollute the tree.
		first := strings.SplitN(rel, "/", 2)[0]
		if first == "__MACOSX" || strings.HasPrefix(filepath.Base(rel), "._") {
			skipCount++
			continue
		}
		// Guard against Zip Slip: reject any segment that climbs out.
		if strings.Contains(rel, "..") {
			skipCount++
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, fmt.Errorf("open zip entry %s: %w", rel, err)
		}
		// Pre-flight size guard: reject this entry before reading if it would
		// push the cumulative total past the zip-bomb cap.
		entrySize := f.UncompressedSize64
		if extracted+int64(entrySize) > maxDecompressedZipBytes {
			_ = rc.Close()
			return nil, nil, fmt.Errorf("zip entry %s: total decompressed size exceeds %d bytes", rel, maxDecompressedZipBytes)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, nil, fmt.Errorf("read zip entry %s: %w", rel, err)
		}
		extracted += int64(len(data))
		if extracted > maxDecompressedZipBytes {
			return nil, nil, fmt.Errorf("zip entry %s: total decompressed size exceeds %d bytes", rel, maxDecompressedZipBytes)
		}
		header, err := buildMultipartFileHeader(filepath.Base(rel), data)
		if err != nil {
			return nil, nil, fmt.Errorf("build header for %s: %w", rel, err)
		}
		headers = append(headers, header)
		relPaths = append(relPaths, rel)
	}
	return headers, relPaths, nil
}

// buildMultipartFileHeader re-packages in-memory file bytes as a genuine
// *multipart.FileHeader by writing a one-part multipart body and parsing it
// with ReadForm. This is the only portable way to produce a header whose
// Open() works without touching multipart's unexported fields.
func buildMultipartFileHeader(filename string, data []byte) (*multipart.FileHeader, error) {
	body := new(bytes.Buffer)
	w := multipart.NewWriter(body)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	// Wrap the buffer as an http request so ReadForm can parse it.
	req, err := http.NewRequest("POST", "", body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if err := req.ParseMultipartForm(int64(len(data)) + 1024); err != nil {
		return nil, fmt.Errorf("parse repackaged multipart: %w", err)
	}
	files := req.MultipartForm.File["file"]
	if len(files) == 0 {
		return nil, errors.New("repackaged multipart has no file")
	}
	return files[0], nil
}

// textprotoMIME is kept for backward-compat with earlier draft; not referenced now.
type textprotoMIME = map[string][]string

// ensure the http package is referenced.
var _ = http.StatusOK
