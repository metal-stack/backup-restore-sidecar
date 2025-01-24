package utils

import "io"

// SequentialWriterAt is wrapper of io.Writer, in order to mock the WriteAt Type, which is used by S3 for uploading
type SequentialWriterAt struct {
	w io.Writer
}

// NewSequentialWriterAt() returns new SequentialWriterAt
func NewSequentialWriterAt(w io.Writer) *SequentialWriterAt {
	return &SequentialWriterAt{w: w}
}

// WriteAt() writes text using io.Writer
func (s *SequentialWriterAt) WriteAt(p []byte, off int64) (n int, err error) {
	return s.w.Write(p)
}
