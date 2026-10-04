package sflow

import "encoding/binary"

// reader is a bounds-checked XDR reader. Every method returns ErrTruncated
// instead of panicking when the input is too short.
type reader struct {
	b   []byte
	off int
}

func (r *reader) remaining() int { return len(r.b) - r.off }

func (r *reader) u32() (uint32, error) {
	if r.remaining() < 4 {
		return 0, errTruncated
	}
	v := binary.BigEndian.Uint32(r.b[r.off:])
	r.off += 4
	return v, nil
}

func (r *reader) u64() (uint64, error) {
	if r.remaining() < 8 {
		return 0, errTruncated
	}
	v := binary.BigEndian.Uint64(r.b[r.off:])
	r.off += 8
	return v, nil
}

func (r *reader) bytes(n int) ([]byte, error) {
	if n < 0 || r.remaining() < n {
		return nil, errTruncated
	}
	v := r.b[r.off : r.off+n]
	r.off += n
	return v, nil
}

// opaque reads n bytes followed by XDR padding to a 4-byte boundary.
func (r *reader) opaque(n uint32) ([]byte, error) {
	if uint64(n) > uint64(r.remaining()) {
		return nil, errTruncated
	}
	v, _ := r.bytes(int(n))
	pad := (4 - int(n)%4) % 4
	if r.remaining() < pad {
		return nil, errTruncated
	}
	r.off += pad
	return v, nil
}

// sub returns a reader over the next n bytes (a length-delimited structure).
func (r *reader) sub(n uint32) (*reader, error) {
	if uint64(n) > uint64(r.remaining()) {
		return nil, errLengthOverflow
	}
	b, _ := r.bytes(int(n))
	return &reader{b: b}, nil
}
