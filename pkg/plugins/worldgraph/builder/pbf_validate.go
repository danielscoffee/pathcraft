package builder

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	maxPBFBlobHeaderSize    = 64 << 10
	maxPBFBlobSize          = 32 << 20
	maxPBFUncompressedBlock = 64 << 20
)

func validatePBF(ctx context.Context, path string, maxWayNodes int) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()

	blockIndex := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var sizeBytes [4]byte
		if _, err := io.ReadFull(file, sizeBytes[:]); err != nil {
			if err == io.EOF && blockIndex > 0 {
				return nil
			}
			return err
		}
		headerSize := binary.BigEndian.Uint32(sizeBytes[:])
		if headerSize == 0 || headerSize >= maxPBFBlobHeaderSize {
			return fmt.Errorf("invalid PBF blob header size %d", headerSize)
		}
		header := make([]byte, headerSize)
		if _, err := io.ReadFull(file, header); err != nil {
			return err
		}
		kind, blobSize, err := parsePBFBlobHeader(header)
		if err != nil {
			return err
		}
		if blobSize < 0 || blobSize >= maxPBFBlobSize {
			return fmt.Errorf("invalid PBF blob size %d", blobSize)
		}
		blob := make([]byte, blobSize)
		if _, err := io.ReadFull(file, blob); err != nil {
			return err
		}
		payload, err := decodeBoundedPBFBlob(blob)
		if err != nil {
			return err
		}
		switch {
		case blockIndex == 0 && kind != "OSMHeader":
			return fmt.Errorf("first PBF block is %q, want OSMHeader", kind)
		case kind == "OSMHeader":
			if blockIndex != 0 {
				return fmt.Errorf("unexpected OSMHeader block")
			}
		case kind == "OSMData":
			if err := validatePBFPrimitiveBlock(payload, maxWayNodes); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unexpected PBF block type %q", kind)
		}
		blockIndex++
	}
}

func parsePBFBlobHeader(data []byte) (kind string, size int, err error) {
	size = -1
	err = eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, varintValue uint64) error {
		switch number {
		case 1:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid PBF blob type field")
			}
			kind = string(bytesValue)
		case 3:
			if fieldType != protowire.VarintType || varintValue > math.MaxInt32 {
				return fmt.Errorf("invalid PBF blob size field")
			}
			size = int(varintValue)
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	if kind == "" || size < 0 {
		return "", 0, fmt.Errorf("incomplete PBF blob header")
	}
	return kind, size, nil
}

func decodeBoundedPBFBlob(data []byte) ([]byte, error) {
	var raw, compressed []byte
	rawPresent, compressedPresent := false, false
	rawSize := -1
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, varintValue uint64) error {
		switch number {
		case 1:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid raw PBF blob")
			}
			raw = bytesValue
			rawPresent = true
		case 2:
			if fieldType != protowire.VarintType || varintValue > math.MaxInt32 {
				return fmt.Errorf("invalid uncompressed PBF block size")
			}
			rawSize = int(varintValue)
		case 3:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid compressed PBF blob")
			}
			compressed = bytesValue
			compressedPresent = true
		case 4, 5:
			return fmt.Errorf("unsupported PBF compression")
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if rawPresent && compressedPresent {
		return nil, fmt.Errorf("PBF blob has multiple encodings")
	}
	if rawPresent {
		if len(raw) > maxPBFUncompressedBlock {
			return nil, fmt.Errorf("uncompressed PBF block exceeds %d bytes", maxPBFUncompressedBlock)
		}
		return raw, nil
	}
	if !compressedPresent || rawSize < 0 {
		return nil, fmt.Errorf("PBF blob has no supported payload")
	}
	if rawSize > maxPBFUncompressedBlock {
		return nil, fmt.Errorf("uncompressed PBF block exceeds %d bytes", maxPBFUncompressedBlock)
	}
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	var decoded bytes.Buffer
	decoded.Grow(rawSize)
	written, copyErr := io.Copy(&decoded, io.LimitReader(reader, maxPBFUncompressedBlock+1))
	closeErr := reader.Close()
	if copyErr != nil {
		return nil, copyErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if written > maxPBFUncompressedBlock {
		return nil, fmt.Errorf("uncompressed PBF block exceeds %d bytes", maxPBFUncompressedBlock)
	}
	if written != int64(rawSize) {
		return nil, fmt.Errorf("uncompressed PBF block is %d bytes, declared %d", written, rawSize)
	}
	return decoded.Bytes(), nil
}

func validatePBFPrimitiveBlock(data []byte, maxWayNodes int) error {
	stringCount := -1
	stringTableSeen := false
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 1:
			if fieldType != protowire.BytesType || stringTableSeen {
				return fmt.Errorf("invalid or repeated PBF string table")
			}
			stringTableSeen = true
			count, err := validatePBFStringTable(bytesValue)
			if err != nil {
				return err
			}
			stringCount = count
		case 2:
			if fieldType != protowire.BytesType || !stringTableSeen {
				return fmt.Errorf("invalid PBF primitive group")
			}
			return validatePBFPrimitiveGroup(bytesValue, stringCount, maxWayNodes)
		}
		return nil
	}); err != nil {
		return err
	}
	if stringCount < 1 {
		return fmt.Errorf("PBF primitive block has no string table")
	}
	return nil
}

func validatePBFStringTable(data []byte) (int, error) {
	count := 0
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		if number != 1 {
			return nil
		}
		if fieldType != protowire.BytesType {
			return fmt.Errorf("invalid PBF string table entry")
		}
		if count == 0 && len(bytesValue) != 0 {
			return fmt.Errorf("PBF string table index zero is not empty")
		}
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

func validatePBFPrimitiveGroup(data []byte, stringCount, maxWayNodes int) error {
	denseSeen := false
	return eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 1:
			return fmt.Errorf("non-dense PBF nodes are unsupported")
		case 2:
			if fieldType != protowire.BytesType || denseSeen {
				return fmt.Errorf("invalid or repeated dense PBF nodes")
			}
			denseSeen = true
			return validatePBFDenseNodes(bytesValue, stringCount)
		case 3:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid PBF way")
			}
			return validatePBFWay(bytesValue, stringCount, maxWayNodes)
		default:
			return nil
		}
	})
}

func validatePBFDenseNodes(data []byte, stringCount int) error {
	var idsData, latsData, lonsData, tagsData []byte
	idsSeen, latsSeen, lonsSeen, infoSeen, tagsSeen := false, false, false, false, false
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 1:
			if err := capturePBFBytesField("dense node IDs", fieldType, bytesValue, &idsData, &idsSeen); err != nil {
				return err
			}
		case 5:
			if fieldType != protowire.BytesType || infoSeen {
				return fmt.Errorf("invalid or repeated dense PBF info")
			}
			infoSeen = true
			if err := validatePBFDenseInfo(bytesValue, stringCount); err != nil {
				return err
			}
		case 8:
			if err := capturePBFBytesField("dense node latitudes", fieldType, bytesValue, &latsData, &latsSeen); err != nil {
				return err
			}
		case 9:
			if err := capturePBFBytesField("dense node longitudes", fieldType, bytesValue, &lonsData, &lonsSeen); err != nil {
				return err
			}
		case 10:
			if err := capturePBFBytesField("dense node tags", fieldType, bytesValue, &tagsData, &tagsSeen); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	ids, err := countPackedPBFVarints(idsData)
	if err != nil {
		return err
	}
	lats, err := countPackedPBFVarints(latsData)
	if err != nil {
		return err
	}
	lons, err := countPackedPBFVarints(lonsData)
	if err != nil {
		return err
	}
	if !idsSeen || !latsSeen || !lonsSeen || ids != lats || ids != lons {
		return fmt.Errorf("dense PBF node columns have mismatched lengths")
	}
	if tagsSeen {
		return validatePBFDenseTags(tagsData, ids, stringCount)
	}
	return nil
}

func validatePBFDenseInfo(data []byte, stringCount int) error {
	userIDsSeen := false
	return eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		if number != 5 {
			return nil
		}
		if fieldType != protowire.BytesType || userIDsSeen {
			return fmt.Errorf("invalid or repeated dense PBF user IDs")
		}
		userIDsSeen = true
		userID := int64(0)
		return eachPackedPBFVarint(bytesValue, func(value uint64) error {
			delta := protowire.DecodeZigZag(value)
			if delta > 0 && userID > math.MaxInt64-delta || delta < 0 && userID < math.MinInt64-delta {
				return fmt.Errorf("dense PBF user string index overflows")
			}
			userID += delta
			if userID < 0 || userID >= int64(stringCount) {
				return fmt.Errorf("dense PBF user string index %d is out of range", userID)
			}
			return nil
		})
	})
}

func validatePBFDenseTags(data []byte, nodeCount, stringCount int) error {
	delimiters := 0
	expectingKey := true
	if err := eachPackedPBFVarint(data, func(value uint64) error {
		if value > math.MaxInt32 {
			return fmt.Errorf("dense PBF tag index is out of range")
		}
		index := int32(value)
		if expectingKey && index == 0 {
			delimiters++
			return nil
		}
		if index < 0 || int(index) >= stringCount {
			return fmt.Errorf("dense PBF tag string index %d is out of range", index)
		}
		expectingKey = !expectingKey
		return nil
	}); err != nil {
		return err
	}
	if !expectingKey || delimiters != nodeCount {
		return fmt.Errorf("dense PBF tag stream is malformed")
	}
	return nil
}

func validatePBFWay(data []byte, stringCount, maxWayNodes int) error {
	var keysData, valuesData, refsData, latsData, lonsData []byte
	keysSeen, valuesSeen, infoSeen, refsSeen, latsSeen, lonsSeen := false, false, false, false, false, false
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 2:
			return capturePBFBytesField("way tag keys", fieldType, bytesValue, &keysData, &keysSeen)
		case 3:
			return capturePBFBytesField("way tag values", fieldType, bytesValue, &valuesData, &valuesSeen)
		case 4:
			if fieldType != protowire.BytesType || infoSeen {
				return fmt.Errorf("invalid or repeated PBF way info")
			}
			infoSeen = true
			return validatePBFInfo(bytesValue, stringCount)
		case 8:
			return capturePBFBytesField("way references", fieldType, bytesValue, &refsData, &refsSeen)
		case 9:
			return capturePBFBytesField("way latitudes", fieldType, bytesValue, &latsData, &latsSeen)
		case 10:
			return capturePBFBytesField("way longitudes", fieldType, bytesValue, &lonsData, &lonsSeen)
		default:
			return nil
		}
	}); err != nil {
		return err
	}
	keys, err := validatePBFStringIndexes(keysData, stringCount)
	if err != nil {
		return err
	}
	values, err := validatePBFStringIndexes(valuesData, stringCount)
	if err != nil {
		return err
	}
	if keysSeen != valuesSeen || keys != values {
		return fmt.Errorf("PBF way tag columns have mismatched lengths")
	}
	refs, err := countPackedPBFVarints(refsData)
	if err != nil {
		return err
	}
	if refs > maxWayNodes {
		return fmt.Errorf("%w: way has %d nodes, limit %d", ErrWayNodeLimit, refs, maxWayNodes)
	}
	latCount, err := countPackedPBFVarints(latsData)
	if err != nil {
		return err
	}
	lonCount, err := countPackedPBFVarints(lonsData)
	if err != nil {
		return err
	}
	if latsSeen && latCount != refs || lonsSeen && lonCount != refs {
		return fmt.Errorf("PBF way coordinate columns have mismatched lengths")
	}
	return nil
}

func capturePBFBytesField(name string, fieldType protowire.Type, value []byte, target *[]byte, seen *bool) error {
	if fieldType != protowire.BytesType || *seen {
		return fmt.Errorf("invalid or repeated PBF %s", name)
	}
	*seen = true
	*target = value
	return nil
}

func validatePBFInfo(data []byte, stringCount int) error {
	return eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, _ []byte, varintValue uint64) error {
		if number == 5 {
			if fieldType != protowire.VarintType || varintValue >= uint64(stringCount) {
				return fmt.Errorf("PBF user string index is out of range")
			}
		}
		return nil
	})
}

func validatePBFStringIndexes(data []byte, stringCount int) (int, error) {
	count := 0
	if err := eachPackedPBFVarint(data, func(value uint64) error {
		if value > math.MaxUint32 || value >= uint64(stringCount) {
			return fmt.Errorf("PBF string-table index %d is out of range", value)
		}
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

func countPackedPBFVarints(data []byte) (int, error) {
	count := 0
	if err := eachPackedPBFVarint(data, func(uint64) error {
		count++
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

func eachPackedPBFVarint(data []byte, visit func(uint64) error) error {
	for len(data) > 0 {
		value, size := protowire.ConsumeVarint(data)
		if size < 0 {
			return protowire.ParseError(size)
		}
		if err := visit(value); err != nil {
			return err
		}
		data = data[size:]
	}
	return nil
}

func eachPBFField(data []byte, visit func(protowire.Number, protowire.Type, []byte, uint64) error) error {
	for len(data) > 0 {
		number, fieldType, tagSize := protowire.ConsumeTag(data)
		if tagSize < 0 {
			return protowire.ParseError(tagSize)
		}
		data = data[tagSize:]
		switch fieldType {
		case protowire.BytesType:
			value, size := protowire.ConsumeBytes(data)
			if size < 0 {
				return protowire.ParseError(size)
			}
			if err := visit(number, fieldType, value, 0); err != nil {
				return err
			}
			data = data[size:]
		case protowire.VarintType:
			value, size := protowire.ConsumeVarint(data)
			if size < 0 {
				return protowire.ParseError(size)
			}
			if err := visit(number, fieldType, nil, value); err != nil {
				return err
			}
			data = data[size:]
		default:
			size := protowire.ConsumeFieldValue(number, fieldType, data)
			if size < 0 {
				return protowire.ParseError(size)
			}
			if err := visit(number, fieldType, nil, 0); err != nil {
				return err
			}
			data = data[size:]
		}
	}
	return nil
}
