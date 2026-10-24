package actresscache

import (
	"context"
	"fmt"
	"time"
)

type jsonSize struct {
	ctx  context.Context
	left int64
	err  error
}

func (s *jsonSize) add(n int64) {
	if s.err != nil {
		return
	}
	if err := s.ctx.Err(); err != nil {
		s.err = err
		return
	}
	if n > s.left {
		s.err = fmt.Errorf("record or encoded byte budget exceeded")
		return
	}
	s.left -= n
}

func (s *jsonSize) raw(value string) { s.add(int64(len(value))) }

func (s *jsonSize) text(value string) {
	s.add(2)
	for _, r := range value {
		if s.err != nil {
			return
		}
		switch r {
		case '"', '\\', '\b', '\f', '\n', '\r', '\t':
			s.add(2)
		case '<', '>', '&', '\u2028', '\u2029':
			s.add(6)
		default:
			switch {
			case r < 0x20:
				s.add(6)
			case r < 0x80:
				s.add(1)
			case r < 0x800:
				s.add(2)
			case r < 0x10000:
				s.add(3)
			default:
				s.add(4)
			}
		}
	}
}

func (s *jsonSize) field(name string, first *bool) {
	if !*first {
		s.add(1)
	}
	*first = false
	s.text(name)
	s.add(1)
}

func measureRecord(ctx context.Context, record Evidence, budget int64) (int64, error) {
	size := jsonSize{ctx: ctx, left: budget}
	size.raw(`{"source":`)
	size.text(record.Source)
	size.raw(`,"source_id":`)
	size.text(record.SourceID)
	size.raw(`,"provenance":{"source_url":`)
	size.text(record.Provenance.SourceURL)
	size.raw(`,"dataset_sha256":`)
	size.text(record.Provenance.DatasetSHA256)
	size.raw(`,"parser_revision":`)
	size.text(record.Provenance.ParserRevision)
	size.raw(`,"observed_at":`)
	size.text(record.Provenance.ObservedAt.Format(time.RFC3339Nano))
	size.raw(`},"metadata":{`)
	first := true
	for _, field := range []struct{ name, value string }{{"first_name", record.Metadata.FirstName}, {"last_name", record.Metadata.LastName}, {"japanese_name", record.Metadata.JapaneseName}} {
		if field.value != "" {
			size.field(field.name, &first)
			size.text(field.value)
		}
	}
	if len(record.Metadata.Aliases) > 0 {
		size.field("aliases", &first)
		size.add(1)
		for i, alias := range record.Metadata.Aliases {
			if i > 0 {
				size.add(1)
			}
			size.text(alias)
		}
		size.add(1)
	}
	if record.Metadata.ThumbnailURL != "" {
		size.field("thumbnail_url", &first)
		size.text(record.Metadata.ThumbnailURL)
	}
	size.add(1)
	if len(record.ExternalIDs) > 0 {
		size.raw(`,"external_ids":[`)
		for i, id := range record.ExternalIDs {
			if i > 0 {
				size.add(1)
			}
			size.raw(`{"namespace":`)
			size.text(id.Namespace)
			size.raw(`,"value":`)
			size.text(id.Value)
			size.add(1)
		}
		size.add(1)
	}
	size.add(1)
	if size.err != nil {
		return 0, size.err
	}
	return budget - size.left, nil
}
