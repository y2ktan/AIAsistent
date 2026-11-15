package agent

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

func buildTestOggPage(headerType byte, granule uint64, serial, seq uint32, segs []byte, body []byte) []byte {
	var page bytes.Buffer
	page.WriteString("OggS")
	page.WriteByte(0)
	page.WriteByte(headerType)
	_ = binary.Write(&page, binary.LittleEndian, granule)
	_ = binary.Write(&page, binary.LittleEndian, serial)
	_ = binary.Write(&page, binary.LittleEndian, seq)
	_ = binary.Write(&page, binary.LittleEndian, uint32(0))
	page.WriteByte(byte(len(segs)))
	page.Write(segs)
	page.Write(body)
	raw := page.Bytes()
	crc := oggCRC32(raw)
	binary.LittleEndian.PutUint32(raw[22:26], crc)
	return raw
}

func TestOggPacketReaderMultiPageAndSpanningPacket(t *testing.T) {
	var stream bytes.Buffer
	const serial = 0x11223344

	// Header pages
	opusHead := []byte("OpusHead\x01\x01\x38\x01\x80\xbb\x00\x00\x00\x00\x00")
	writeOggPage(&stream, 0x02, 0, serial, 0, opusHead)
	writeOggPage(&stream, 0x00, 0, serial, 1, []byte("OpusTags\x04\x00\x00\x00test\x00\x00\x00\x00"))

	pkt1 := []byte{0xd8, 1, 2, 3}
	pkt2 := []byte{0xd8, 4, 5, 6, 7}
	// pkt3 spans page 1 -> page 2 (255 + 10 = 265 bytes)
	pkt3 := bytes.Repeat([]byte{0xab}, 265)
	pkt3[0] = 0xd8
	pkt4 := []byte{0xd8, 8, 9}
	pkt5 := []byte{0xd8, 10, 11, 12}

	// Page 1: pkt1, pkt2, first 255 bytes of pkt3
	var body1 bytes.Buffer
	body1.Write(pkt1)
	body1.Write(pkt2)
	body1.Write(pkt3[:255])
	stream.Write(buildTestOggPage(0x00, 1920, serial, 2, []byte{byte(len(pkt1)), byte(len(pkt2)), 255}, body1.Bytes()))

	// Page 2: remaining 10 bytes of pkt3 (continued), pkt4
	var body2 bytes.Buffer
	body2.Write(pkt3[255:])
	body2.Write(pkt4)
	stream.Write(buildTestOggPage(0x01, 3840, serial, 3, []byte{10, byte(len(pkt4))}, body2.Bytes()))

	// Page 3: pkt5 (EOS)
	stream.Write(buildTestOggPage(0x04, 4800, serial, 4, []byte{byte(len(pkt5))}, pkt5))

	reader := NewOggPacketReader(&stream)
	var got [][]byte
	for {
		pkt, err := reader.NextPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got = append(got, pkt)
	}

	want := [][]byte{pkt1, pkt2, pkt3, pkt4, pkt5}
	if len(got) != len(want) {
		t.Fatalf("got %d packets, want %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Fatalf("packet %d mismatch: got len %d, want len %d", i, len(got[i]), len(want[i]))
		}
	}
}

func TestOpusPacketDuration(t *testing.T) {
	cases := []struct {
		name string
		pkt  []byte
		want time.Duration
	}{
		{"celt 2.5ms 1-frame", []byte{16 << 3}, 2500 * time.Microsecond},
		{"celt 5ms 1-frame", []byte{17 << 3}, 5 * time.Millisecond},
		{"silk 10ms 1-frame", []byte{0 << 3}, 10 * time.Millisecond},
		{"celt 20ms 1-frame", []byte{27 << 3}, 20 * time.Millisecond},
		{"silk 40ms 1-frame", []byte{2 << 3}, 40 * time.Millisecond},
		{"silk 60ms 1-frame", []byte{3 << 3}, 60 * time.Millisecond},
		{"hybrid 20ms 2-frame code1", []byte{(13 << 3) | 1}, 40 * time.Millisecond},
		{"celt 10ms 2-frame code2", []byte{(18 << 3) | 2}, 20 * time.Millisecond},
		{"celt 20ms 3-frame code3", []byte{(27 << 3) | 3, 3}, 60 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OpusPacketDuration(tc.pkt); got != tc.want {
				t.Fatalf("OpusPacketDuration(%x) = %v, want %v", tc.pkt, got, tc.want)
			}
		})
	}
}
