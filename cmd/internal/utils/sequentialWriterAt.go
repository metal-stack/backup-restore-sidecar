package utils

import "io"

type SequentialWriterAt struct {
	w io.Writer
}

func NewSequentialWriterAt(w io.Writer) *SequentialWriterAt {
	return &SequentialWriterAt{w: w}
}

func (s *SequentialWriterAt) WriteAt(p []byte, off int64) (n int, err error) {
	return s.w.Write(p)
}
