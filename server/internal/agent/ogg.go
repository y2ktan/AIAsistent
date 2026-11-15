package agent

import (
	"bytes"
	"encoding/binary"
	"sync"
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
