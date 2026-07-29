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
	var groups [][]byte
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 1:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid PBF string table")
			}
			count, err := validatePBFStringTable(bytesValue)
			if err != nil {
				return err
			}
			stringCount = count
		case 2:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid PBF primitive group")
			}
			groups = append(groups, bytesValue)
		}
		return nil
	}); err != nil {
		return err
	}
	if stringCount < 1 {
		return fmt.Errorf("PBF primitive block has no string table")
	}
	for _, group := range groups {
		if err := validatePBFPrimitiveGroup(group, stringCount, maxWayNodes); err != nil {
			return err
		}
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
	return eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 1:
			return fmt.Errorf("non-dense PBF nodes are unsupported")
		case 2:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid dense PBF nodes")
			}
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
	var idFields, latFields, lonFields, keyValueFields [][]byte
	var denseInfo []byte
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		if fieldType != protowire.BytesType {
			return nil
		}
		switch number {
		case 1:
			idFields = append(idFields, bytesValue)
		case 5:
			denseInfo = bytesValue
		case 8:
			latFields = append(latFields, bytesValue)
		case 9:
			lonFields = append(lonFields, bytesValue)
		case 10:
			keyValueFields = append(keyValueFields, bytesValue)
		}
		return nil
	}); err != nil {
		return err
	}
	ids, err := countPackedPBFVarints(idFields)
	if err != nil {
		return err
	}
	lats, err := countPackedPBFVarints(latFields)
	if err != nil {
		return err
	}
	lons, err := countPackedPBFVarints(lonFields)
	if err != nil {
		return err
	}
	if len(idFields) == 0 || len(latFields) == 0 || len(lonFields) == 0 || ids != lats || ids != lons {
		return fmt.Errorf("dense PBF node columns have mismatched lengths")
	}
	if denseInfo != nil {
		if err := validatePBFDenseInfo(denseInfo, stringCount); err != nil {
			return err
		}
	}
	if len(keyValueFields) > 0 {
		return validatePBFDenseTags(keyValueFields, ids, stringCount)
	}
	return nil
}

func validatePBFDenseInfo(data []byte, stringCount int) error {
	return eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		if number != 5 || fieldType != protowire.BytesType {
			return nil
		}
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

func validatePBFDenseTags(fields [][]byte, nodeCount, stringCount int) error {
	delimiters := 0
	expectingKey := true
	for _, field := range fields {
		if err := eachPackedPBFVarint(field, func(value uint64) error {
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
	}
	if !expectingKey || delimiters != nodeCount {
		return fmt.Errorf("dense PBF tag stream is malformed")
	}
	return nil
}

func validatePBFWay(data []byte, stringCount, maxWayNodes int) error {
	var keyFields, valueFields, refFields, latFields, lonFields [][]byte
	var infoFields [][]byte
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		if fieldType != protowire.BytesType {
			return nil
		}
		switch number {
		case 2:
			keyFields = append(keyFields, bytesValue)
		case 3:
			valueFields = append(valueFields, bytesValue)
		case 4:
			infoFields = append(infoFields, bytesValue)
		case 8:
			refFields = append(refFields, bytesValue)
		case 9:
			latFields = append(latFields, bytesValue)
		case 10:
			lonFields = append(lonFields, bytesValue)
		}
		return nil
	}); err != nil {
		return err
	}
	keys, err := validatePBFStringIndexes(keyFields, stringCount)
	if err != nil {
		return err
	}
	values, err := validatePBFStringIndexes(valueFields, stringCount)
	if err != nil {
		return err
	}
	if (len(keyFields) == 0) != (len(valueFields) == 0) || keys != values {
		return fmt.Errorf("PBF way tag columns have mismatched lengths")
	}
	for _, info := range infoFields {
		if err := validatePBFInfo(info, stringCount); err != nil {
			return err
		}
	}
	refs, err := countPackedPBFVarints(refFields)
	if err != nil {
		return err
	}
	if refs > maxWayNodes {
		return fmt.Errorf("%w: way has %d nodes, limit %d", ErrWayNodeLimit, refs, maxWayNodes)
	}
	latCount, err := countPackedPBFVarints(latFields)
	if err != nil {
		return err
	}
	lonCount, err := countPackedPBFVarints(lonFields)
	if err != nil {
		return err
	}
	if len(latFields) > 0 && latCount != refs || len(lonFields) > 0 && lonCount != refs {
		return fmt.Errorf("PBF way coordinate columns have mismatched lengths")
	}
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

func validatePBFStringIndexes(fields [][]byte, stringCount int) (int, error) {
	count := 0
	for _, field := range fields {
		if err := eachPackedPBFVarint(field, func(value uint64) error {
			if value > math.MaxUint32 || value >= uint64(stringCount) {
				return fmt.Errorf("PBF string-table index %d is out of range", value)
			}
			count++
			return nil
		}); err != nil {
			return 0, err
		}
	}
	return count, nil
}

func countPackedPBFVarints(fields [][]byte) (int, error) {
	count := 0
	for _, field := range fields {
		if err := eachPackedPBFVarint(field, func(uint64) error {
			count++
			return nil
		}); err != nil {
			return 0, err
		}
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
