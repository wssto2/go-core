// Package upload validates and stores multipart uploads.
package upload

import (
	"fmt"
	"io"
	"mime/multipart"

	"github.com/gabriel-vasile/mimetype"
	"github.com/wssto2/go-core/apperr"
)

const bytesPerMB = 1024 * 1024

// ValidateHeader checks a multipart file header against a size limit in bytes and an allow-list of MIME types, for a
// handler that already holds the header (a typed route's multipart input) and so cannot use ValidateFile. It sniffs the
// content's own type with mimetype, never the client's header, and seeks back to the start. On success it returns the
// opened file and its detected MIME type, and the caller closes the file. On a failure the file is closed here and nil
// is returned: 400 for a size or a type that is not allowed, 500 for a file that cannot be read.
func ValidateHeader(
	fileHeader *multipart.FileHeader,
	maxSizeBytes int64,
	allowedMIMETypes map[string]bool,
) (multipart.File, string, error) {
	if fileHeader.Size > maxSizeBytes {
		return nil, "", apperr.BadRequest(fmt.Sprintf("file size exceeds %d MB", maxSizeBytes/bytesPerMB))
	}

	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", apperr.Internal(err)
	}

	mtype, err := mimetype.DetectReader(file)
	if err != nil {
		closeQuietly(file)

		return nil, "", apperr.Internal(err)
	}

	if !allowedMIMETypes[mtype.String()] {
		closeQuietly(file)

		return nil, "", apperr.BadRequest("unsupported file type: " + mtype.String())
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		closeQuietly(file)

		return nil, "", apperr.Internal(err)
	}

	return file, mtype.String(), nil
}

// closeQuietly closes a file on a failure path: the error being returned is the one that matters.
func closeQuietly(file io.Closer) {
	_ = file.Close() // the validation error is returned; a close error here is secondary
}
