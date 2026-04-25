// Package recording reads ProtoStreamV1 session recordings into
// per-session features. The format is small enough to re-implement here:
// see upstream-repo/lib/events/stream.go (NewProtoReader, ~line 933) for
// the canonical version, and the layout doc at stream.go:65-74 for the
// header. Re-implementing avoids pulling in lib/events' transitive deps
// (trace, clockwork, lib/utils, lib/session, lib/defaults).
package recording

import (
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	apievents "github.com/gravitational/teleport/api/types/events"
)

const (
	int32Size                   = 4
	int64Size                   = 8
	protoStreamV1               = 1
	protoStreamV1PartHeaderSize = int64Size * 3
	maxProtoMessageSizeBytes    = 64 * 1024
)

// reader is a minimal ProtoStreamV1 reader. The format is documented
// inline in upstream lib/events/stream.go around lines 65-74 and 1035-1170.
type reader struct {
	src       io.Reader
	gz        *gzip.Reader
	limited   io.Reader
	padding   int64
	atEOF     bool
	scratch   [maxProtoMessageSizeBytes]byte
	lastIndex int64
}

func newReader(src io.Reader) *reader {
	return &reader{src: src, lastIndex: -1}
}

// next returns the next AuditEvent or io.EOF.
func (r *reader) next(ctx context.Context) (apievents.AuditEvent, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.atEOF {
			return nil, io.EOF
		}
		if r.gz == nil {
			if err := r.openPart(); err != nil {
				return nil, err
			}
		}
		ev, err := r.readRecord()
		if err == nil {
			return ev, nil
		}
		if !errors.Is(err, io.EOF) {
			return nil, err
		}
		// End of this part. Drain any dangling bytes in the gzip section
		// (older Teleport versions could pad inside it), then advance past
		// the inter-part padding bytes.
		if _, derr := io.Copy(io.Discard, r.gz); derr != nil {
			return nil, fmt.Errorf("drain gzip: %w", derr)
		}
		if cerr := r.gz.Close(); cerr != nil {
			return nil, fmt.Errorf("close gzip: %w", cerr)
		}
		// Drain remainder of the limited reader (padding inside the part
		// boundary that the gzip layer didn't consume).
		if _, derr := io.Copy(io.Discard, r.limited); derr != nil {
			return nil, fmt.Errorf("drain part: %w", derr)
		}
		if r.padding > 0 {
			if _, derr := io.CopyN(io.Discard, r.src, r.padding); derr != nil {
				return nil, fmt.Errorf("skip padding: %w", derr)
			}
			r.padding = 0
		}
		r.gz = nil
		r.limited = nil
	}
}

func (r *reader) openPart() error {
	var hdr [protoStreamV1PartHeaderSize]byte
	if _, err := io.ReadFull(r.src, hdr[:]); err != nil {
		if errors.Is(err, io.EOF) {
			r.atEOF = true
			return io.EOF
		}
		return fmt.Errorf("read part header: %w", err)
	}
	version := binary.BigEndian.Uint64(hdr[0:int64Size])
	if version != protoStreamV1 {
		return fmt.Errorf("unsupported protocol version %d", version)
	}
	partSize := binary.BigEndian.Uint64(hdr[int64Size : 2*int64Size])
	r.padding = int64(binary.BigEndian.Uint64(hdr[2*int64Size : 3*int64Size]))
	r.limited = io.LimitReader(r.src, int64(partSize))
	gz, err := gzip.NewReader(r.limited)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	r.gz = gz
	return nil
}

func (r *reader) readRecord() (apievents.AuditEvent, error) {
	for {
		var sizeBuf [int32Size]byte
		if _, err := io.ReadFull(r.gz, sizeBuf[:]); err != nil {
			return nil, err
		}
		messageSize := binary.BigEndian.Uint32(sizeBuf[:])
		if messageSize == 0 {
			return nil, fmt.Errorf("unexpected message size 0")
		}
		if int(messageSize) > len(r.scratch) {
			return nil, fmt.Errorf("record size %d exceeds %d", messageSize, len(r.scratch))
		}
		if _, err := io.ReadFull(r.gz, r.scratch[:messageSize]); err != nil {
			return nil, fmt.Errorf("read record: %w", err)
		}
		var oneof apievents.OneOf
		if err := oneof.Unmarshal(r.scratch[:messageSize]); err != nil {
			return nil, fmt.Errorf("unmarshal OneOf: %w", err)
		}
		ev, err := apievents.FromOneOf(oneof)
		if err != nil {
			return nil, fmt.Errorf("FromOneOf: %w", err)
		}
		idx := ev.GetIndex()
		if idx <= r.lastIndex {
			// Duplicate / out-of-order — same logic as upstream ProtoReader.
			continue
		}
		r.lastIndex = idx
		return ev, nil
	}
}
