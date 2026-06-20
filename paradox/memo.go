package paradox

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
)

const mbBlockSize = 4096

// ReadMemoData reads memo content from the .MB file given a MemoPointer
func ReadMemoData(dbPath string, mp *MemoPointer) (string, error) {
	if mp == nil || (mp.BlockIndex == 0 && len(mp.InlineData) == 0) {
		return "", nil
	}

	if mp.BlockIndex == 0 {
		return strings.TrimRight(string(mp.InlineData), "\x00"), nil
	}

	// Build .MB path from .DB path
	ext := filepath.Ext(dbPath)
	mbPath := dbPath[:len(dbPath)-len(ext)] + ".MB"

	f, err := os.Open(mbPath)
	if err != nil {
		mbPath = dbPath[:len(dbPath)-len(ext)] + ".mb"
		f, err = os.Open(mbPath)
		if err != nil {
			if len(mp.InlineData) > 0 {
				return strings.TrimRight(string(mp.InlineData), "\x00"), nil
			}
			return "", nil
		}
	}
	defer f.Close()

	// Decode blockIndex: high bits = MB block number, low 12 bits = sub-index
	blockNum := int64(mp.BlockIndex) / mbBlockSize
	subIndex := int(mp.BlockIndex) % mbBlockSize

	// Seek to the block
	blockOffset := blockNum * mbBlockSize
	blockData := make([]byte, mbBlockSize)
	_, err = f.Seek(blockOffset, 0)
	if err != nil {
		return "", err
	}
	n, err := f.Read(blockData)
	if err != nil || n < mbBlockSize {
		if len(mp.InlineData) > 0 {
			return strings.TrimRight(string(mp.InlineData), "\x00"), nil
		}
		return "", err
	}

	blockType := blockData[0]

	if blockType == 3 {
		// Type 3: sub-allocated block
		// Entry number = 0x40 - sub_index (1-based)
		entryNum := 0x40 - subIndex

		// Pointer table at block offset 0x100, 5-byte entries
		// Entry format: [mod(1)] [offset_div16(1)] [size_chunks(1)] [entry_number(1)] [reserved(1)]
		ptBase := 0x100
		maxEntries := (0x150 - ptBase) / 5 // pointer table fits between 0x100 and 0x150

		dataOffInBlock := -1
		for i := 0; i < maxEntries; i++ {
			eOff := ptBase + i*5
			if eOff+5 > len(blockData) {
				break
			}
			eNum := int(blockData[eOff+3])
			if eNum == 0 && blockData[eOff+1] == 0 {
				break // end of table
			}
			if eNum == entryNum {
				dataOffInBlock = int(blockData[eOff+1]) * 16
				break
			}
		}

		if dataOffInBlock >= 0 && mp.DataLength > 0 {
			dataLen := int(mp.DataLength)
			if dataOffInBlock+dataLen <= len(blockData) {
				result := string(blockData[dataOffInBlock : dataOffInBlock+dataLen])
				return strings.TrimRight(result, "\x00"), nil
			}
		}
	} else if blockType == 2 {
		// Type 2: single blob block
		dataLen := binary.LittleEndian.Uint32(blockData[4:8])
		if dataLen == 0 {
			return "", nil
		}
		startOff := 8
		endOff := startOff + int(dataLen)
		if endOff > len(blockData) {
			totalBlocks := int(blockData[1])
			if totalBlocks < 1 {
				totalBlocks = 1
			}
			allData := make([]byte, totalBlocks*mbBlockSize)
			f.Seek(blockOffset, 0)
			f.Read(allData)
			endOff = startOff + int(dataLen)
			if endOff > len(allData) {
				endOff = len(allData)
			}
			return strings.TrimRight(string(allData[startOff:endOff]), "\x00"), nil
		}
		return strings.TrimRight(string(blockData[startOff:endOff]), "\x00"), nil
	}

	// Fallback: return inline data if available
	if len(mp.InlineData) > 0 {
		return strings.TrimRight(string(mp.InlineData), "\x00"), nil
	}
	return "", nil
}
