package zip

import "io"

type OffsetReader struct {
	r      io.Reader
	offset int64
	read   int64
}

func NewOffsetReader(r io.Reader, offset int64) *OffsetReader {
	return &OffsetReader{
		r:      r,
		offset: offset,
	}
}

func (o *OffsetReader) Read(p []byte) (n int, err error) {
	if o.read < o.offset {
		skip := o.offset - o.read
		var discarded int64
		for skip > 0 {
			discarded, err = io.CopyN(io.Discard, o.r, skip)
			if err != nil {
				return 0, err
			}
			o.read += discarded
			skip -= discarded
		}
	}

	n, err = o.r.Read(p)
	if err != nil {
		o.read += int64(n)
	}
	return n, err
}
