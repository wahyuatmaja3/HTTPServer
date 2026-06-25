package paradox

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"
)

// Paradox field types
const (
	FieldAlpha     byte = 0x01
	FieldDate      byte = 0x02
	FieldShort     byte = 0x03
	FieldLong      byte = 0x04
	FieldCurrency  byte = 0x05
	FieldNumber    byte = 0x06
	FieldLogical   byte = 0x09
	FieldMemoBlob  byte = 0x0C
	FieldBlob      byte = 0x0D
	FieldFmtMemo   byte = 0x0E
	FieldOLE       byte = 0x0F
	FieldGraphic   byte = 0x10
	FieldTime      byte = 0x14
	FieldTimestamp byte = 0x15
	FieldAutoInc   byte = 0x16
	FieldBCD       byte = 0x17
	FieldBytes     byte = 0x18
)

// FieldInfo describes one field in a Paradox table
type FieldInfo struct {
	Name string
	Type byte
	Size uint16
}

// Header is the parsed Paradox .DB file header
type Header struct {
	RecordSize    uint16
	HeaderSize    uint16
	FileType      byte
	MaxTableSize  byte
	NumRecords    uint32
	NumFields     uint16
	NumKeyFields  uint16
	SortOrderID   uint16
	Fields        []FieldInfo
	DataBlockSize int // in bytes, based on MaxTableSize
}

// Record is a map of field name to value
type Record map[string]interface{}

// paradoxEpoch is Jan 1, 0001 for Paradox date fields
// Paradox dates are stored as number of days since Jan 1, 0001
var paradoxEpoch = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)

// ParseFieldValue parses a raw byte slice for a given field type
func ParseFieldValue(data []byte, field FieldInfo) interface{} {
	size := int(field.Size)
	if len(data) < size {
		return nil
	}
	raw := data[:size]

	switch field.Type {
	case FieldAlpha:
		return strings.TrimRight(string(raw), "\x00")
	case FieldDate:
		if size < 4 {
			return nil
		}
		return parseDate(raw)
	case FieldShort:
		if size < 2 {
			return nil
		}
		return parseShort(raw)
	case FieldLong, FieldAutoInc:
		if size < 4 {
			return nil
		}
		return parseLong(raw)
	case FieldNumber, FieldCurrency:
		if size < 8 {
			return nil
		}
		return parseNumber(raw)
	case FieldLogical:
		if size < 1 {
			return nil
		}
		if raw[0] == 0 {
			return nil
		}
		// In Paradox, 0x80 = false after XOR, but actually:
		// stored value 0x80 XOR => could mean false
		// We treat nonzero-after-clearing-high-bit as true
		return raw[0] != 0x80
	case FieldMemoBlob, FieldFmtMemo, FieldBlob, FieldOLE, FieldGraphic:
		return parseMemoPointer(raw, size)
	case FieldTime:
		if size < 4 {
			return nil
		}
		return parseTime(raw)
	case FieldTimestamp:
		if size < 8 {
			return nil
		}
		return parseTimestamp(raw)
	case FieldBCD:
		return fmt.Sprintf("%x", raw)
	case FieldBytes:
		return raw
	default:
		return fmt.Sprintf("%x", raw)
	}
}

// MemoPointer holds info to look up memo data in .MB file
type MemoPointer struct {
	BlockIndex uint32
	EntryIndex uint16
	DataLength uint32
	InlineData []byte
}

func parseMemoPointer(raw []byte, size int) *MemoPointer {
	if size < 10 {
		return nil
	}
	// Memo fields store: last bytes have index (4 bytes LE), mod count, data offset, data length
	// Layout in the field (at end of field data):
	// [inline data...][4-byte block index LE][1-byte data offset within block (in 16-byte units)][1-byte mod count low][2-byte data length LE]
	// Actually Paradox memo format:
	// Last 10 bytes: blockIndex(4 LE), dataOffset(1), modCount(1), dataLength(2 LE), then 2 more?
	// Let's use the standard layout:
	// offset size-10: 4 bytes block index
	// offset size-6: 1 byte modifier/data_offset
	// offset size-5: 1 byte modifier
	// offset size-4: 2 bytes length of data in memo block
	// offset size-2: 2 bytes ???

	// Actually simpler: the memo pointer is in the last bytes
	// For a 10-byte memo field: all of it is pointer
	// block_index = LE uint32 at [0:4]
	// data_size = LE uint16 at [4:6] (or somewhere)
	// mod_count = LE uint16 at [6:8]
	// Let me look at actual data patterns

	// From the hex dump of API.DB memo fields (SQL field):
	// Looking at the first record's SQL field area ending:
	// The standard Paradox memo field format for level 7:
	// If data fits in field, it's inline. Otherwise:
	// Last 4 bytes: block number (LE int32), rest is inline text that didn't fit
	// Actually for Paradox 7:
	// Memo field stores data inline if it fits
	// Last 10 bytes are: [hdrOffset(4)][dataLength(4)][modCount(2)]
	// Where hdrOffset is the offset into the .MB file

	// The 10-byte memo pointer sits at the end of the field. Layout:
	//   [0:4] block index in the .MB file (LE uint32)
	//   [4:8] length of the memo data (LE uint32)
	//   [8:10] entry index within a type-3 (sub-allocated) block (LE uint16)
	// Any bytes before the pointer are inline data (used only as a fallback).
	mp := &MemoPointer{}

	idx := size - 10
	if idx < 0 {
		idx = 0
	}
	mp.BlockIndex = binary.LittleEndian.Uint32(raw[idx : idx+4])
	mp.DataLength = binary.LittleEndian.Uint32(raw[idx+4 : idx+8])
	mp.EntryIndex = binary.LittleEndian.Uint16(raw[idx+8 : idx+10])

	// If there's inline data before the pointer
	if idx > 0 {
		mp.InlineData = make([]byte, idx)
		copy(mp.InlineData, raw[:idx])
	}

	return mp
}

func parseDate(raw []byte) interface{} {
	// Paradox date: 4-byte signed int, high bit flipped for sorting
	v := flipHighBit32(raw)
	if v == 0 {
		return nil
	}
	// v = number of days since Jan 1, year 1 (0001-01-01)
	d := paradoxEpoch.AddDate(0, 0, int(v)-1)
	return d.Format("02/01/2006")
}

func parseShort(raw []byte) interface{} {
	v := flipHighBit16(raw)
	if raw[0] == 0 && raw[1] == 0 {
		return nil
	}
	return int16(v)
}

func parseLong(raw []byte) interface{} {
	v := flipHighBit32(raw)
	if raw[0] == 0 && raw[1] == 0 && raw[2] == 0 && raw[3] == 0 {
		return nil
	}
	return int32(v)
}

func parseNumber(raw []byte) interface{} {
	// Paradox number: 8-byte double with high bit flipped
	// If first byte has high bit set after reading, flip all bits (negative)
	if allZero(raw) {
		return nil
	}
	buf := make([]byte, 8)
	copy(buf, raw)
	if buf[0] & 0x80 != 0 {
		// Positive: just flip the high bit
		buf[0] ^= 0x80
	} else {
		// Negative: flip all bits
		for i := range buf {
			buf[i] ^= 0xFF
		}
	}
	bits := binary.BigEndian.Uint64(buf)
	return math.Float64frombits(bits)
}

func parseTime(raw []byte) interface{} {
	v := flipHighBit32(raw)
	if v == 0 {
		return nil
	}
	// Milliseconds since midnight
	ms := int32(v)
	h := ms / 3600000
	ms %= 3600000
	m := ms / 60000
	ms %= 60000
	s := ms / 1000
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func parseTimestamp(raw []byte) interface{} {
	if allZero(raw) {
		return nil
	}
	// 8-byte double, high bit flipped, representing days+fraction since epoch
	buf := make([]byte, 8)
	copy(buf, raw)
	if buf[0] & 0x80 != 0 {
		buf[0] ^= 0x80
	} else {
		for i := range buf {
			buf[i] ^= 0xFF
		}
	}
	bits := binary.BigEndian.Uint64(buf)
	v := math.Float64frombits(bits)

	// v is milliseconds since Jan 1, 0001
	ms := int64(v)
	days := ms / 86400000
	remainder := ms % 86400000

	t := paradoxEpoch.AddDate(0, 0, int(days)-1)
	t = t.Add(time.Duration(remainder) * time.Millisecond)
	return t.Format("02/01/2006 15:04:05")
}

func flipHighBit32(raw []byte) uint32 {
	buf := make([]byte, 4)
	copy(buf, raw)
	buf[0] ^= 0x80
	return binary.BigEndian.Uint32(buf)
}

func flipHighBit16(raw []byte) uint16 {
	buf := make([]byte, 2)
	copy(buf, raw)
	buf[0] ^= 0x80
	return binary.BigEndian.Uint16(buf)
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
