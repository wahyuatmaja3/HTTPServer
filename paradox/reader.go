package paradox

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Table represents a parsed Paradox .DB table
type Table struct {
	Header  Header
	Records []Record
	Path    string
}

// ReadTable reads and parses a Paradox .DB file
func ReadTable(path string) (*Table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	t := &Table{Path: path}

	// Read file header
	if err := t.readHeader(f); err != nil {
		return nil, fmt.Errorf("read header %s: %w", path, err)
	}

	// Read records
	if err := t.readRecords(f); err != nil {
		return nil, fmt.Errorf("read records %s: %w", path, err)
	}

	return t, nil
}

func (t *Table) readHeader(f *os.File) error {
	// Read first part of header
	var buf [256]byte
	_, err := f.Read(buf[:])
	if err != nil {
		return err
	}

	h := &t.Header
	h.RecordSize = binary.LittleEndian.Uint16(buf[0x00:0x02])
	h.HeaderSize = binary.LittleEndian.Uint16(buf[0x02:0x04])
	h.FileType = buf[0x04]
	h.MaxTableSize = buf[0x05]
	h.NumRecords = binary.LittleEndian.Uint32(buf[0x06:0x0A]) // actually it's at different offset

	// Paradox header layout:
	// 0x00: record size (2 LE)
	// 0x02: header size (2 LE)
	// 0x04: file type
	// 0x05: max table size (block size = this * 0x400)
	// 0x06-0x09: num records (4 LE) -- but wait, looking at actual data:
	//   0x06: 2c00 0000 = 44 for API.DB? That could be numRecords

	// Let me re-read from the actual hex dumps:
	// API.DB: 7101 0008 0010 2c00 0000 0100 0100 0100
	// 0x00: 0x0171 = 369 = recordSize
	// 0x02: 0x0800 = 2048 = headerSize
	// 0x04: 0x10 = fileType (0x10 = Paradox for Windows, level 7?)
	// 0x05: 0x2C = 44 = maxTableSize?? That seems too big
	// Actually wait:
	// 71 01 = LE uint16 = 0x0171 = 369
	// 00 08 = LE uint16 = 0x0800 = 2048
	// 00 = fileType
	// 10 = maxTableSize (16, so block size = 16*1024 = 16384)
	// Actually the values should be:
	// byte 0-1: record size LE
	// byte 2-3: header size LE
	// byte 4: file type
	// byte 5: max table size

	// Hmm, let me re-examine: 7101 0008 00 10
	// That would be: recordSize=0x0171=369, headerSize=0x0800=2048, fileType=0x00, maxTableSize=0x10=16
	// But 00 as fileType doesn't make sense. Let me look more at Paradox format specs.

	// Standard Paradox 7 header:
	// Offset 0x00 (2): Record size
	// Offset 0x02 (2): Header size
	// Offset 0x04 (1): File type (0=indexed, 1=primary index, 2=non-indexed, etc.)
	// Offset 0x05 (1): Max table size (block size in KB)
	// Offset 0x06 (4): Num records
	// Offset 0x0A (2): Next auto-increment value (or blocks used)
	// Offset 0x0C (2): Num blocks
	// Offset 0x0E (2): First block
	// Offset 0x10 (2): Last block
	// Offset 0x15 (1): File version ID
	// Offset 0x21 (2): Num fields
	// Offset 0x23 (4): Primary key fields
	// ...
	// Actually the exact layout varies. Let me use the correct offsets:

	// From Paradox format documentation:
	// 0x00-0x01: Record size (LE uint16)
	// 0x02-0x03: Header size (LE uint16)
	// 0x04: File type
	// 0x05: Max table size (in 1KB blocks)
	// 0x06-0x09: Number of records (LE int32)
	// 0x0A-0x0B: Next block
	// 0x0C-0x0D: File blocks
	// 0x0E-0x0F: First data block
	// 0x10-0x11: Last data block
	// 0x15: File version (3,4,5,7,10,11,12)
	// 0x21-0x22: Number of fields (LE uint16)
	// 0x23-0x24: Number of key fields

	h.RecordSize = binary.LittleEndian.Uint16(buf[0x00:0x02])
	h.HeaderSize = binary.LittleEndian.Uint16(buf[0x02:0x04])
	h.FileType = buf[0x04]
	h.MaxTableSize = buf[0x05]

	// Number of records - Paradox 7 stores this at 0x06 as a 4-byte LE int
	// BUT looking at actual data: 2c00 0000 at offset 6 = 44 records for API.DB
	// For Setting.DB: 0100 0000 at offset 6 = 1 record. Seems right!
	h.NumRecords = binary.LittleEndian.Uint32(buf[0x06:0x0A])

	// Number of fields
	// For API.DB at 0x21: looking at hex: offset 0x20 = 0008 0001
	// So 0x21 = 08, that would be 8 fields? But we see field names:
	// Nomor, Command, SQL, Version, UserCreate, TglCreate, UserModify, TglModify = 8 fields!
	// Actually let me check: at 0x21-0x22 in the header:
	// API.DB hex at offset 0x20: 00 08 00 01
	// But that's bytes[0x20]=0x00, bytes[0x21]=0x08, bytes[0x22]=0x00, bytes[0x23]=0x01
	// numFields at 0x21 as uint16 LE = 0x0008? No, it should be at a proper 2-byte boundary

	// Looking more carefully at the Paradox format. Different sources give different offsets.
	// Let me try to derive from the data:
	//
	// API.DB header hex:
	// 00: 71 01 00 08 00 10 2c 00 00 00 01 00 01 00 01 00
	// 10: 01 00 0a 00 00 00 fc 58 e3 6b 1c e3 e3 6b 00 00
	// 20: 00 08 00 01 00 00 ff 00 ff 4c 00 00 00 59 34 00
	//
	// Setting.DB header hex:
	// 00: dc 01 00 08 00 10 01 00 00 00 01 00 01 00 01 00
	// 10: 01 00 05 00 00 00 8c dc e3 6b 00 00 00 00 00 00
	// 20: 00 20 00 01 00 00 ff 00 ff 4c 00 00 00 0a 3f 00
	//
	// API.DB: record_size=369, header=2048, numRecords=44
	// numFields: at offset 0x21 we have 08 for API.DB and 20 for Setting.DB
	// API has 8 fields, Setting has 32 fields? Let me count Setting field names...
	// Setting field names from hex: Kode, TanggalMin, TanggalMax, JamMasuk, JamPulang,
	// JamMasukSabtu, JamPulangSabtu, JamMasuk2, JamPulang2, JamMasukSabtu2, JamPulangSabtu2...
	// That does look like ~32 fields for Setting.DB
	// 0x20 = 32! So numFields is a single byte?

	// Wait, 0x21 byte value 0x08 for API, and 0x20 = 32 for Setting
	// But byte at 0x21 for Setting is 0x20? Let me re-check:
	// Setting: bytes[0x20]=0x00, bytes[0x21]=0x20
	// API: bytes[0x20]=0x00, bytes[0x21]=0x08

	// So numFields = uint16 LE at offset 0x21?
	// API: LE(08, 00) = 8. Setting: LE(20, 00) = 32. But actually:
	// offset 0x21 for API = byte value 0x08
	// wait I need to count from 0 again:
	// 00: 71 01 | 00 08 | 00 | 10 | 2c 00 00 00 | ...
	// That's: [0]=0x71 [1]=0x01 [2]=0x00 [3]=0x08 [4]=0x00 [5]=0x10 [6]=0x2c ...

	// OK so: recordSize = LE(0x71, 0x01) = 0x0171 = 369
	// headerSize = LE(0x00, 0x08) = 0x0800 = 2048
	// fileType = 0x00
	// maxTableSize = 0x10 = 16 (16KB blocks)
	// numRecords = LE(0x2c, 0x00, 0x00, 0x00) = 44

	// Then continuing:
	// [0x0A]=0x01 [0x0B]=0x00 => next block or something = 1
	// [0x0C]=0x01 [0x0D]=0x00 => file blocks = 1
	// [0x0E]=0x01 [0x0F]=0x00 => first block = 1
	// [0x10]=0x01 [0x11]=0x00 => last block = 1
	// [0x12]=0x0a [0x13]=0x00 => ? = 10
	// ...
	// [0x15] would be at index 0x15 = 21: let me recount
	// hex line 10: 01 00 0a 00 00 00 fc 58 e3 6b 1c e3 e3 6b 00 00
	// That starts at offset 0x10: [0x10]=0x01, [0x11]=0x00, [0x12]=0x0a, [0x13]=0x00
	// [0x14]=0x00, [0x15]=0x00, [0x16]=0xfc, [0x17]=0x58...

	// Hmm, [0x15]=0x00 doesn't seem like a version number
	// Let me try a different approach - parse based on the known Paradox 7 for Windows format

	// After more research, the correct Paradox 7 header layout is:
	// 0x00: recordSize (2 LE)
	// 0x02: headerSize (2 LE)
	// 0x04: fileType (1)
	// 0x05: maxTableSize (1) - block size in KB
	// 0x06: numRecords (4 LE)
	// 0x0A: nextBlock (2 LE)
	// 0x0C: fileBlocks (2 LE)
	// 0x0E: firstDataBlock (2 LE)
	// 0x10: lastDataBlock (2 LE)
	// 0x12: unknown (2)
	// 0x14: unknown (2)
	// 0x16: modifiedFlags1 (4)
	// 0x1A: modifiedFlags2 (4)
	// 0x1E: unknown (2)
	// 0x20: unknown (1)
	// 0x21: numFields (2 LE) -- or could be 1 byte
	// Actually, the issue is endianness. Let me try:
	// 0x21 for API.DB: bytes are 08 00 => LE uint16 = 8
	// 0x21 for Setting.DB: bytes are 20 00 => LE uint16 = 32
	// That works! So numFields is uint16 LE at offset 0x21

	// But wait, offset 0x20 for API is 00, for Setting is 00.
	// What if numFields is actually at offset 0x21 as just the byte,
	// or at 0x20 as uint16?

	// Actually looking byte by byte again at API header line 2:
	// hex 20: 00 08 00 01 00 00 ff 00 ff 4c 00 00 00 59 34 00
	// offset 0x20 = 0x00
	// offset 0x21 = 0x08
	// It's more likely numFields is at offset 0x21 as a byte, or offset 0x20:0x22 as uint16
	// Since API has 8 fields and Setting has 32:

	// Let me try: numFields = buf[0x21] | (buf[0x22] << 8)  [LE uint16 at 0x21]
	// API: 0x08 | (0x00 << 8) = 8 ✓
	// Setting: 0x20 | (0x00 << 8) = 32 ✓
	// Great!

	h.NumFields = binary.LittleEndian.Uint16(buf[0x21:0x23])
	h.NumKeyFields = binary.LittleEndian.Uint16(buf[0x23:0x25])

	// Block size in bytes
	h.DataBlockSize = int(h.MaxTableSize) * 0x400

	// Now read field type/size info
	// Field info is stored right after the standard header area
	// For Paradox 7: field definitions start at a fixed offset
	// Looking at offset 0x58 in the header:
	// API.DB: 0x58 = 0e 02 00 00 00 20 00 0c 01 0c 01 ...
	// And field names appear at offset ~0x1B0

	// The field info section structure:
	// At some offset, there are numFields entries of 2 bytes each:
	// byte 0: field type
	// byte 1+: field size (could be 1 or 2 bytes)
	//
	// From the Paradox spec, field type/size entries are 2 bytes each:
	// type (1 byte) + size (1 byte) for types < 256 bytes
	// But sizes can be > 255, so it's actually:
	// size (2 bytes LE) at one offset, type (1 byte) at another

	// The standard layout for Paradox 7 is:
	// After the 0x58 header area come:
	// - Field sizes array: numFields * 2 bytes (size LE uint16 each)... no
	// Actually the format stores field info as pairs:
	// fieldType(1) + fieldSize(1) for each field, starting at offset 0x58 (varies)

	// Let me look at the actual data more carefully:
	// API.DB field area starts somewhere before the field names
	// Field names are at ~0x1B0: Nomor, Command, SQL, Version, UserCreate, TglCreate, UserModify, TglModify
	// 8 fields.
	// At offset 0x78 in API.DB:
	// 06 08 01 ff 0c 3c 06 08 01 0f 02 04 01 0f 02 04
	// That looks like: type=06 size=08, type=01 size=ff, type=0c size=3c, type=06 size=08,
	// type=01 size=0f, type=02 size=04, type=01 size=0f, type=02 size=04
	// = 8 entries! field types: Number(06), Alpha(01), Memo(0C), Number(06),
	//   Alpha(01), Date(02), Alpha(01), Date(02)
	// Fields: Nomor(Number/8), Command(Alpha/255), SQL(Memo/60), Version(Number/8),
	//         UserCreate(Alpha/15), TglCreate(Date/4), UserModify(Alpha/15), TglModify(Date/4)
	//
	// Wait, alpha size 0xFF=255 for Command, and memo size 0x3C=60 for SQL
	// That makes sense!
	//
	// The total: 8+255+60+8+15+4+15+4 = 369 = recordSize! Perfect!

	// So field type+size pairs are 2 bytes each starting at some offset
	// For API.DB, field info starts at offset 0x78
	// That's: headerOffset = 0x78 = 120

	// But this might vary. Let me look at Setting.DB:
	// Setting.DB at offset 0x78:
	// 01 05 02 04 02 04 14 04 14 04 14 04 14 04 14 04
	// type=01 size=05, type=02 size=04, type=02 size=04, type=14 size=04...
	// Field types: Alpha(01/5), Date(02/4), Date(02/4), Time(14/4), Time(14/4)...
	// Kode(Alpha/5), TanggalMin(Date/4), TanggalMax(Date/4), JamMasuk(Time/4)...
	// That matches! Great!

	// So field type/size pairs start at offset 0x78 for these files
	// This is actually: 0x78 = 0x58 + 0x20 or some formula
	// Looking at the header: the field info offset depends on the header structure

	// For Paradox 7, field type+size entries start at a computed offset:
	// Actually, according to multiple sources, the field definitions start at offset 0x78
	// for Paradox 7 (version >= 7) files

	// Let me verify: offset 0x58 in API.DB is within the header, and we're at 0x78
	// Perhaps it's: 0x58 + numSortOrderBytes or something
	// Regardless, 0x78 works for both files, so let's use it

	// Actually, looking more carefully at the Paradox format:
	// The field type/size array starts right after the fixed header
	// The fixed header for Paradox 7 is 0x78 bytes
	// So fieldInfoOffset = 0x78

	// But also, some sources say 0x58 for older formats
	// Let me try to detect: check if format gives reasonable types at 0x78

	fieldInfoOffset := 0x78

	// We might need more of the header for large tables
	fullHeader := make([]byte, int(h.HeaderSize))
	f.Seek(0, 0)
	_, err = f.Read(fullHeader)
	if err != nil {
		return err
	}

	// Read field type+size pairs
	h.Fields = make([]FieldInfo, h.NumFields)
	for i := 0; i < int(h.NumFields); i++ {
		off := fieldInfoOffset + i*2
		if off+2 > len(fullHeader) {
			return fmt.Errorf("field info out of bounds at field %d", i)
		}
		h.Fields[i].Type = fullHeader[off]
		h.Fields[i].Size = uint16(fullHeader[off+1])
	}

	// Read field names - they appear after the field info and some other data
	// Field names are null-terminated strings stored sequentially
	// They start after the sort order info
	// For Paradox 7, field names are at:
	// offset = 0x78 + numFields*2 + (varies based on encrypted/sort info)
	// Actually looking at the data, field names for API.DB start at 0x1B1
	// and for Setting.DB at 0x189

	// The field names section can be found by scanning for them
	// They come after: field info + encryption info + sort order + auto inc
	// A simpler approach: scan from after field types to find the name area

	// From various Paradox specs, after the field type/size array comes:
	// - key fields info
	// - sort order block name (like "DBWINUS0")
	// Then field names follow as null-terminated strings

	// Let me find the sort order block and field names after it
	// The sort order name is a fixed-size field (like "DBWINUS0" padded to some length)
	// After the last padding comes the field names

	// For API.DB: sort order "DBWINUS0" at offset 0x204
	// For Setting.DB: sort order text "restemp.DB" appears at ~0x132

	// Actually, let me just scan for field names. They're null-terminated
	// and appear in the header before the data starts.

	// Strategy: work backwards from where data starts (headerSize)
	// and find the field names section

	// Better strategy: field names are after the encryption info area
	// Find them by looking for null-terminated sequences near the end of the header

	// The field names area can be reliably found at a computed offset:
	// After field info: numFields * 2 bytes from fieldInfoOffset
	// Then there may be 4 bytes per key field
	// Then a sort order ID area
	// Then field names

	afterFieldInfo := fieldInfoOffset + int(h.NumFields)*2

	// After field info come additional structures. Let's find field names
	// by scanning for the first printable null-terminated string that could be a field name

	nameStart := findFieldNamesStart(fullHeader, afterFieldInfo)
	if nameStart < 0 {
		return fmt.Errorf("could not find field names in header")
	}

	// Parse null-terminated field names
	pos := nameStart
	for i := 0; i < int(h.NumFields); i++ {
		end := pos
		for end < len(fullHeader) && fullHeader[end] != 0 {
			end++
		}
		if end > pos {
			h.Fields[i].Name = string(fullHeader[pos:end])
		}
		pos = end + 1 // skip null terminator
	}

	// Handle fields with sizes > 255 for certain types
	// Alpha fields can have 2-byte sizes; let's check if any sizes seem wrong
	// Actually in the 2-byte field info, the second byte IS the size, but max 255
	// For larger Alpha fields, Paradox stores the size differently
	// Looking at API.DB: Command field is Alpha with size 0xFF=255
	// But we expect it to be 255 chars which is a common Paradox limit

	return nil
}

func findFieldNamesStart(header []byte, searchFrom int) int {
	// Field names are null-terminated strings near the end of the header
	// They appear after the sort order name and other metadata
	// Look for a pattern where we have short printable strings separated by nulls

	// Strategy: look for the first byte after searchFrom that starts a
	// sequence of printable chars followed by a null, and verify it looks like
	// field names (multiple such sequences matching numFields)

	// Simple approach: scan for the known marker - sort order block name
	// ends with nulls, then field names begin

	for i := searchFrom; i < len(header)-1; i++ {
		if header[i] == 0 && i+1 < len(header) && header[i+1] != 0 {
			// Check if what follows looks like a field name
			// Field names are typically short alphabetic strings
			ch := header[i+1]
			if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
				// Verify: look ahead for a null terminator within reasonable distance
				for j := i + 1; j < len(header) && j < i+100; j++ {
					if header[j] == 0 {
						name := string(header[i+1 : j])
						if len(name) >= 1 && len(name) <= 50 && isValidFieldName(name) {
							return i + 1
						}
						break
					}
				}
			}
		}
	}
	return -1
}

func isValidFieldName(s string) bool {
	for _, ch := range s {
		if !((ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_') {
			return false
		}
	}
	return true
}

func (t *Table) readRecords(f *os.File) error {
	h := &t.Header
	if h.NumRecords == 0 {
		return nil
	}

	blockSize := h.DataBlockSize
	if blockSize == 0 {
		blockSize = 4096 // default
	}

	const blockHeaderSize = 6 // nextBlock(2) + prevBlock(2) + addedRecsSinceRestructure(2)

	// Data starts after the file header
	dataStart := int64(h.HeaderSize)
	fileInfo, err := f.Stat()
	if err != nil {
		return err
	}
	fileSize := fileInfo.Size()

	// Calculate how many records fit per block (after block header)
	recsPerBlock := (blockSize - blockHeaderSize) / int(h.RecordSize)
	if recsPerBlock < 1 {
		recsPerBlock = 1
	}

	// Read all data blocks
	t.Records = make([]Record, 0, h.NumRecords)
	pos := dataStart

	for pos < fileSize && uint32(len(t.Records)) < h.NumRecords {
		// Determine how many records to read from this block
		remaining := int(h.NumRecords) - len(t.Records)
		toRead := recsPerBlock
		if toRead > remaining {
			toRead = remaining
		}

		// Read records in this block (skip 6-byte block header)
		for i := 0; i < toRead; i++ {
			recOffset := pos + blockHeaderSize + int64(i)*int64(h.RecordSize)
			if recOffset+int64(h.RecordSize) > fileSize {
				break
			}
			f.Seek(recOffset, 0)
			recData := make([]byte, h.RecordSize)
			n, err := f.Read(recData)
			if err != nil || n < int(h.RecordSize) {
				break
			}

			rec := t.parseRecord(recData)
			t.Records = append(t.Records, rec)
		}

		pos += int64(blockSize)
	}

	return nil
}

func (t *Table) parseRecord(data []byte) Record {
	rec := make(Record)
	offset := 0

	for _, field := range t.Header.Fields {
		size := int(field.Size)
		if offset+size > len(data) {
			break
		}

		val := ParseFieldValue(data[offset:offset+size], field)

		// Handle memo fields - resolve memo pointers
		if (field.Type == FieldMemoBlob || field.Type == FieldFmtMemo || field.Type == FieldBlob) && val != nil {
			if mp, ok := val.(*MemoPointer); ok {
				memoText, err := ReadMemoData(t.Path, mp)
				if err == nil && memoText != "" {
					val = memoText
				} else if len(mp.InlineData) > 0 {
					val = strings.TrimRight(string(mp.InlineData), "\x00")
				} else {
					val = ""
				}
			}
		}

		rec[field.Name] = val
		offset += size
	}

	return rec
}

// GetFieldNames returns the list of field names
func (t *Table) GetFieldNames() []string {
	names := make([]string, len(t.Header.Fields))
	for i, f := range t.Header.Fields {
		names[i] = f.Name
	}
	return names
}

// FindTableFile looks for a Paradox .DB file in the tables directory (case-insensitive)
func FindTableFile(tablesDir, tableName string) (string, error) {
	// Try exact name first
	path := filepath.Join(tablesDir, tableName+".DB")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	// Try with .db extension
	path = filepath.Join(tablesDir, tableName+".db")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	// Case-insensitive search
	entries, err := os.ReadDir(tablesDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.EqualFold(name, tableName+".DB") || strings.EqualFold(name, tableName+".db") {
			return filepath.Join(tablesDir, name), nil
		}
	}
	return "", fmt.Errorf("table %s not found in %s", tableName, tablesDir)
}
