package agent

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"
)

var (
	oggCRCOnce  sync.Once
	oggCRCTable [256]uint32
)

func initOggCRC() {
	for i := 0; i < 256; i++ {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = (r << 1) ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		oggCRCTable[i] = r
	}
}

func oggCRC32(data []byte) uint32 {
	oggCRCOnce.Do(initOggCRC)
	var crc uint32
	for _, b := range data {
		crc = (crc << 8) ^ oggCRCTable[byte(crc>>24)^b]
	}
	return crc
}

func writeOggPage(buf *bytes.Buffer, headerType byte, granule uint64, serial, seq uint32, packet []byte) {
	var segs []byte
	rem := len(packet)
	for rem >= 255 {
		segs = append(segs, 255)
		rem -= 255
	}
	segs = append(segs, byte(rem))

	var page bytes.Buffer
	page.WriteString("OggS")
	page.WriteByte(0) // stream_structure_version
	page.WriteByte(headerType)
	_ = binary.Write(&page, binary.LittleEndian, granule)
	_ = binary.Write(&page, binary.LittleEndian, serial)
	_ = binary.Write(&page, binary.LittleEndian, seq)
	_ = binary.Write(&page, binary.LittleEndian, uint32(0)) // CRC placeholder
	page.WriteByte(byte(len(segs)))
	page.Write(segs)
	page.Write(packet)

	raw := page.Bytes()
	crc := oggCRC32(raw)
	binary.LittleEndian.PutUint32(raw[22:26], crc)
	buf.Write(raw)
}

// EncodeOggOpus wraps 48kHz 20ms Opus RTP payloads into a valid Ogg/Opus stream.
func EncodeOggOpus(frames [][]byte) []byte {
	var buf bytes.Buffer
	const serial uint32 = 0x61656769 // "aegi"

	// 1. OpusHead (BOS)
	opusHead := []byte{
		'O', 'p', 'u', 's', 'H', 'e', 'a', 'd',
		1,          // version
		1,          // channel count
		0x38, 0x01, // pre-skip (312 samples LE)
		0x80, 0xbb, 0x00, 0x00, // input sample rate 48000 Hz LE
		0x00, 0x00, // output gain
		0,          // channel mapping family
	}
	writeOggPage(&buf, 0x02, 0, serial, 0, opusHead)

	// 2. OpusTags
	vendor := []byte("aegis-voice")
	var tags bytes.Buffer
	tags.WriteString("OpusTags")
	_ = binary.Write(&tags, binary.LittleEndian, uint32(len(vendor)))
	tags.Write(vendor)
	_ = binary.Write(&tags, binary.LittleEndian, uint32(0)) // user comment count
	writeOggPage(&buf, 0x00, 0, serial, 1, tags.Bytes())

	// 3. Audio frames (960 samples per 20ms frame at 48kHz)
	var granule uint64
	for i, frame := range frames {
		if len(frame) == 0 {
			continue
		}
		granule += 960
		var hType byte
		if i == len(frames)-1 {
			hType = 0x04 // EOS
		}
		writeOggPage(&buf, hType, granule, serial, uint32(i+2), frame)
	}

	return buf.Bytes()
}

// OggPacketReader reads an Ogg stream page by page and yields individual
// non-header Opus packets, joining packets that span page boundaries.
type OggPacketReader struct {
	r      io.Reader
	buf    bytes.Buffer
	queue  [][]byte
	header [27]byte
}

func NewOggPacketReader(r io.Reader) *OggPacketReader {
	return &OggPacketReader{r: r}
}

func (o *OggPacketReader) NextPacket() ([]byte, error) {
	for len(o.queue) == 0 {
		if _, err := io.ReadFull(o.r, o.header[:]); err != nil {
			return nil, err
		}
		if string(o.header[:4]) != "OggS" {
			return nil, errors.New("invalid ogg page signature")
		}
		numSegs := int(o.header[26])
		segs := make([]byte, numSegs)
		if _, err := io.ReadFull(o.r, segs); err != nil {
			return nil, err
		}
		var bodyLen int
		for _, s := range segs {
			bodyLen += int(s)
		}
		body := make([]byte, bodyLen)
		if _, err := io.ReadFull(o.r, body); err != nil {
			return nil, err
		}

		pos := 0
		for _, s := range segs {
			n := int(s)
			o.buf.Write(body[pos : pos+n])
			pos += n
			if s < 255 {
				pkt := make([]byte, o.buf.Len())
				copy(pkt, o.buf.Bytes())
				o.buf.Reset()
				if bytes.HasPrefix(pkt, []byte("OpusHead")) || bytes.HasPrefix(pkt, []byte("OpusTags")) {
					continue
				}
				if len(pkt) > 0 {
					o.queue = append(o.queue, pkt)
				}
			}
		}
	}

	pkt := o.queue[0]
	o.queue = o.queue[1:]
	return pkt, nil
}

// OpusPacketDuration returns the total duration of an Opus packet from its TOC header (RFC 6716).
func OpusPacketDuration(pkt []byte) time.Duration {
	if len(pkt) == 0 {
		return 20 * time.Millisecond
	}
	toc := pkt[0]
	config := (toc >> 3) & 0x1f
	var frameDur time.Duration
	switch {
	case config <= 11:
		switch config % 4 {
		case 0:
			frameDur = 10 * time.Millisecond
		case 1:
			frameDur = 20 * time.Millisecond
		case 2:
			frameDur = 40 * time.Millisecond
		case 3:
			frameDur = 60 * time.Millisecond
		}
	case config <= 15:
		if config&1 == 0 {
			frameDur = 10 * time.Millisecond
		} else {
			frameDur = 20 * time.Millisecond
		}
	default:
		switch config % 4 {
		case 0:
			frameDur = 2500 * time.Microsecond
		case 1:
			frameDur = 5 * time.Millisecond
		case 2:
			frameDur = 10 * time.Millisecond
		case 3:
			frameDur = 20 * time.Millisecond
		}
	}

	frames := 1
	switch toc & 0x03 {
	case 1, 2:
		frames = 2
	case 3:
		if len(pkt) >= 2 {
			frames = int(pkt[1] & 0x3f)
			if frames == 0 {
				frames = 1
			}
		}
	}
	return frameDur * time.Duration(frames)
}

