package gmtool

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
)

const detailIndexVersion = 3
const detailHeaderSize = 128
const detailRecordSize = 48
const maxItemDetailBytes = 8 << 20
const detailIndexMagic = "GMITEM02"

func itemDetailIndexPath(summaryPath, checksum string) string {
	if checksum == "" || summaryPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(summaryPath), fmt.Sprintf("item-details-v%d-%s.bin", detailIndexVersion, checksum))
}

func openItemDetailIndex(path, checksum string) (*os.File, []byte, error) {
	if path == "" || checksum == "" {
		return nil, nil, os.ErrNotExist
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	fail := func(e error) (*os.File, []byte, error) { f.Close(); return nil, nil, e }
	header := make([]byte, detailHeaderSize)
	if _, err = f.ReadAt(header, 0); err != nil {
		return fail(err)
	}
	size, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	count := binary.LittleEndian.Uint64(header[40:48])
	offset := binary.LittleEndian.Uint64(header[48:56])
	if string(header[:8]) != detailIndexMagic || hex.EncodeToString(header[8:40]) != checksum || binary.LittleEndian.Uint64(header[64:72]) != detailIndexVersion || count > 10_000_000 || offset < detailHeaderSize || offset > uint64(size.Size()) || offset+count*detailRecordSize != uint64(size.Size()) || binary.LittleEndian.Uint64(header[56:64]) != uint64(size.Size()) {
		return fail(errors.New("item detail index header mismatch"))
	}
	return f, header, nil
}

// Binary search reads at most one record per step; only the requested JSON
// projection is allocated. The full text/detail corpus never becomes a Go map.
func readItemDetailIndex(ctx context.Context, path, checksum string, id uint32) (*itemProjection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f, h, err := openItemDetailIndex(path, checksum)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	indexOffset := binary.LittleEndian.Uint64(h[48:56])
	low, high := uint64(0), binary.LittleEndian.Uint64(h[40:48])
	var record [detailRecordSize]byte
	for low < high {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		mid := low + (high-low)/2
		if _, err = f.ReadAt(record[:], int64(indexOffset+mid*detailRecordSize)); err != nil {
			return nil, err
		}
		found := binary.LittleEndian.Uint32(record[:4])
		if found < id {
			low = mid + 1
			continue
		}
		if found > id {
			high = mid
			continue
		}
		length := binary.LittleEndian.Uint32(record[4:8])
		offset := binary.LittleEndian.Uint64(record[8:16])
		if length == 0 || length > maxItemDetailBytes || offset < detailHeaderSize || offset+uint64(length) > indexOffset {
			return nil, errors.New("invalid item detail record bounds")
		}
		raw := make([]byte, length)
		if _, err = f.ReadAt(raw, int64(offset)); err != nil {
			return nil, err
		}
		hash := sha256.Sum256(raw)
		if string(hash[:]) != string(record[16:]) {
			return nil, errors.New("item detail checksum mismatch")
		}
		decoder, e := zlib.NewReader(bytes.NewReader(raw))
		if e != nil {
			return nil, e
		}
		decoded, e := io.ReadAll(io.LimitReader(decoder, maxItemDetailBytes+1))
		decoder.Close()
		if e != nil || len(decoded) > maxItemDetailBytes {
			return nil, errors.New("invalid compressed item detail")
		}
		var value itemProjection
		if err = json.Unmarshal(decoded, &value); err != nil {
			return nil, err
		}
		projectedID, err := projectionItemID(&value)
		if err != nil || projectedID != id || value.PVFChecksum != checksum {
			return nil, errors.New("item detail identity mismatch")
		}
		return &value, ctx.Err()
	}
	return nil, os.ErrNotExist
}

type ItemDetailBuildReport struct {
	Path        string `json:"path"`
	PVFChecksum string `json:"pvfChecksum"`
	Items       int    `json:"items"`
	Failed      int    `json:"failed"`
	Reused      bool   `json:"reused"`
	Bytes       int64  `json:"bytes"`
}

// BuildItemDetails performs an offline, sequential compilation. A validated
// archive digest and parser version identify each immutable index generation.
func BuildItemDetails(ctx context.Context, source dnfpvf.Source, summaryPath string, log func(string, ...any)) (ItemDetailBuildReport, error) {
	if log == nil {
		log = func(string, ...any) {}
	}
	checksum := itemIndexSourceChecksum(source)
	report := ItemDetailBuildReport{Path: itemDetailIndexPath(summaryPath, checksum), PVFChecksum: checksum}
	if report.Path == "" {
		return report, errors.New("item details require an archive SHA256 and summary path")
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if f, h, err := openItemDetailIndex(report.Path, checksum); err == nil {
		hash := sha256.New()
		offset := binary.LittleEndian.Uint64(h[48:56])
		_, err = io.Copy(hash, io.NewSectionReader(f, int64(offset), int64(binary.LittleEndian.Uint64(h[56:64])-offset)))
		indexOK := err == nil && string(hash.Sum(nil)) == string(h[72:104])
		hash.Reset()
		_, err = io.Copy(hash, io.NewSectionReader(f, detailHeaderSize, int64(offset-detailHeaderSize)))
		f.Close()
		if indexOK && err == nil && string(hash.Sum(nil)[:24]) == string(h[104:128]) {
			report.Reused = true
			report.Items = int(binary.LittleEndian.Uint64(h[40:48]))
			report.Bytes = int64(binary.LittleEndian.Uint64(h[56:64]))
			return report, ctx.Err()
		}
	}
	if err := os.MkdirAll(filepath.Dir(report.Path), 0700); err != nil {
		return report, err
	}
	s := &Server{cfg: Config{Logger: log}, deps: Deps{PVF: func() (dnfpvf.Source, error) { return source, nil }}, pvf: newPVFCache()}
	ix := newItemIndex(summaryPath, s, log)
	s.items = ix
	ix.start(ctx)
	if !ix.isReady() {
		return report, ix.failure()
	}
	f, err := os.CreateTemp(filepath.Dir(report.Path), ".item-details-*")
	if err != nil {
		return report, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	defer f.Close()
	if _, err = f.Write(make([]byte, detailHeaderSize)); err != nil {
		return report, err
	}
	records := make([]byte, 0, len(ix.entries)*detailRecordSize)
	offset := uint64(detailHeaderSize)
	dataHash := sha256.New()
	var compressed bytes.Buffer
	encoder, _ := zlib.NewWriterLevel(&compressed, zlib.BestSpeed)
	for _, entry := range ix.snapshot() {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		value, e := s.projectItem(ctx, source, entry)
		if e != nil {
			report.Failed++
			log("item detail omitted", "itemId", entry.ID, "error", e)
			continue
		}
		raw, e := json.Marshal(value)
		if e != nil || len(raw) > maxItemDetailBytes {
			return report, fmt.Errorf("item %d detail exceeds format limits", entry.ID)
		}
		compressed.Reset()
		encoder.Reset(&compressed)
		if _, err = encoder.Write(raw); err != nil {
			return report, err
		}
		if err = encoder.Close(); err != nil {
			return report, err
		}
		raw = compressed.Bytes()
		if len(raw) > maxItemDetailBytes {
			return report, fmt.Errorf("item %d compressed detail exceeds format limits", entry.ID)
		}
		var record [detailRecordSize]byte
		binary.LittleEndian.PutUint32(record[:4], entry.ID)
		binary.LittleEndian.PutUint32(record[4:8], uint32(len(raw)))
		binary.LittleEndian.PutUint64(record[8:16], offset)
		hash := sha256.Sum256(raw)
		copy(record[16:], hash[:])
		if _, err = f.Write(raw); err != nil {
			return report, err
		}
		dataHash.Write(raw)
		offset += uint64(len(raw))
		records = append(records, record[:]...)
		report.Items++
	}
	if report.Items == 0 {
		return report, errors.New("no readable item details")
	}
	if _, err = f.Write(records); err != nil {
		return report, err
	}
	header := make([]byte, detailHeaderSize)
	copy(header[:8], detailIndexMagic)
	digest, _ := hex.DecodeString(checksum)
	copy(header[8:40], digest)
	binary.LittleEndian.PutUint64(header[40:48], uint64(report.Items))
	binary.LittleEndian.PutUint64(header[48:56], offset)
	report.Bytes = int64(offset) + int64(len(records))
	binary.LittleEndian.PutUint64(header[56:64], uint64(report.Bytes))
	binary.LittleEndian.PutUint64(header[64:72], detailIndexVersion)
	hash := sha256.Sum256(records)
	copy(header[72:104], hash[:])
	copy(header[104:128], dataHash.Sum(nil)[:24])
	if _, err = f.WriteAt(header, 0); err != nil {
		return report, err
	}
	if err = f.Sync(); err != nil {
		return report, err
	}
	if err = f.Close(); err != nil {
		return report, err
	}
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if err = os.Rename(tmp, report.Path); err != nil {
		return report, err
	}
	return report, nil
}
