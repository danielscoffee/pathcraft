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
	maxPBFBlobHeaderSize     = 64 << 10
	maxPBFBlobSize           = 32 << 20
	maxPBFUncompressedBlock  = 64 << 20
	maxPBFEntitiesPerBlock   = 500_000
	maxPBFHeaderBlockBytes   = 1 << 20
	maxPBFHeaderFeatures     = 1_024
	maxPBFHeaderStringBytes  = 64 << 10
	maxPBFStringTableEntries = 250_000
	maxPBFStringTableBytes   = 16 << 20
	maxPBFStringLength       = 1 << 20
	maxPBFPrimitiveGroups    = 10_000
	maxPBFTagsPerEntity      = 1_024
	maxPBFTagsPerBlock       = 1_000_000
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
	return validatePBFReader(ctx, file, maxWayNodes)
}

func validatePBFReader(ctx context.Context, reader io.Reader, maxWayNodes int) error {
	if ctx == nil {
		return fmt.Errorf("PBF validation context is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if reader == nil {
		return fmt.Errorf("PBF reader is nil")
	}
	if maxWayNodes < 1 {
		return fmt.Errorf("maximum way node count must be positive")
	}
	blockIndex := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var sizeBytes [4]byte
		if _, err := io.ReadFull(reader, sizeBytes[:]); err != nil {
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
		if _, err := io.ReadFull(reader, header); err != nil {
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
		if _, err := io.ReadFull(reader, blob); err != nil {
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
			if err := validatePBFHeaderBlock(payload); err != nil {
				return err
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
	typeSeen, indexSeen, sizeSeen := false, false, false
	err = eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, varintValue uint64) error {
		switch number {
		case 1:
			if fieldType != protowire.BytesType || typeSeen {
				return fmt.Errorf("invalid or repeated PBF blob type field")
			}
			typeSeen = true
			kind = string(bytesValue)
		case 2:
			if fieldType != protowire.BytesType || indexSeen {
				return fmt.Errorf("invalid or repeated PBF blob index field")
			}
			indexSeen = true
		case 3:
			if fieldType != protowire.VarintType || sizeSeen || varintValue > math.MaxInt32 {
				return fmt.Errorf("invalid or repeated PBF blob size field")
			}
			sizeSeen = true
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
	rawPresent, rawSizePresent, compressedPresent := false, false, false
	rawSize := -1
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, varintValue uint64) error {
		switch number {
		case 1:
			if fieldType != protowire.BytesType || rawPresent {
				return fmt.Errorf("invalid or repeated raw PBF blob")
			}
			raw = bytesValue
			rawPresent = true
		case 2:
			if fieldType != protowire.VarintType || rawSizePresent || varintValue > math.MaxInt32 {
				return fmt.Errorf("invalid or repeated uncompressed PBF block size")
			}
			rawSizePresent = true
			rawSize = int(varintValue)
		case 3:
			if fieldType != protowire.BytesType || compressedPresent {
				return fmt.Errorf("invalid or repeated compressed PBF blob")
			}
			compressed = bytesValue
			compressedPresent = true
		case 4, 5, 6, 7:
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

func validatePBFHeaderBlock(data []byte) error {
	if len(data) > maxPBFHeaderBlockBytes {
		return fmt.Errorf("PBF header block exceeds %d bytes", maxPBFHeaderBlockBytes)
	}
	var singletonSeen [35]bool
	features, stringBytes := 0, 0
	return eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 1:
			if fieldType != protowire.BytesType || singletonSeen[number] {
				return fmt.Errorf("invalid or repeated PBF header bounding box")
			}
			singletonSeen[number] = true
			return validatePBFHeaderBBox(bytesValue)
		case 4, 5:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid PBF header feature")
			}
			features++
			if features > maxPBFHeaderFeatures {
				return fmt.Errorf("PBF header has more than %d features", maxPBFHeaderFeatures)
			}
		case 16, 17, 34:
			if fieldType != protowire.BytesType || singletonSeen[number] {
				return fmt.Errorf("invalid or repeated PBF header string field %d", number)
			}
			singletonSeen[number] = true
		case 32, 33:
			if fieldType != protowire.VarintType || singletonSeen[number] {
				return fmt.Errorf("invalid or repeated PBF header scalar field %d", number)
			}
			singletonSeen[number] = true
		default:
			return nil
		}
		if len(bytesValue) > maxPBFHeaderStringBytes {
			return fmt.Errorf("PBF header string exceeds %d bytes", maxPBFHeaderStringBytes)
		}
		stringBytes += len(bytesValue)
		if stringBytes > maxPBFHeaderBlockBytes {
			return fmt.Errorf("PBF header strings exceed %d bytes", maxPBFHeaderBlockBytes)
		}
		return nil
	})
}

func validatePBFHeaderBBox(data []byte) error {
	var seen [5]bool
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, _ []byte, _ uint64) error {
		if number < 1 || number > 4 {
			return nil
		}
		if fieldType != protowire.VarintType || seen[number] {
			return fmt.Errorf("invalid or repeated PBF header bounding-box field %d", number)
		}
		seen[number] = true
		return nil
	}); err != nil {
		return err
	}
	for number := 1; number <= 4; number++ {
		if !seen[number] {
			return fmt.Errorf("PBF header bounding box is incomplete")
		}
	}
	return nil
}

func validatePBFPrimitiveBlock(data []byte, maxWayNodes int) error {
	stringCount := -1
	stringTableSeen := false
	var scalarSeen [21]bool
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
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid PBF primitive group")
			}
		case 17, 18, 19, 20:
			if fieldType != protowire.VarintType || scalarSeen[number] {
				return fmt.Errorf("invalid or repeated PBF primitive block field %d", number)
			}
			scalarSeen[number] = true
		}
		return nil
	}); err != nil {
		return err
	}
	if stringCount < 1 {
		return fmt.Errorf("PBF primitive block has no string table")
	}
	groups, entities, tags := 0, 0, 0
	return eachPBFField(data, func(number protowire.Number, _ protowire.Type, bytesValue []byte, _ uint64) error {
		if number != 2 {
			return nil
		}
		groups++
		if groups > maxPBFPrimitiveGroups {
			return fmt.Errorf("PBF block has more than %d primitive groups", maxPBFPrimitiveGroups)
		}
		entityCount, tagCount, err := validatePBFPrimitiveGroup(bytesValue, stringCount, maxWayNodes)
		if err != nil {
			return err
		}
		entities += entityCount
		tags += tagCount
		if entities > maxPBFEntitiesPerBlock {
			return fmt.Errorf("PBF block has more than %d entities", maxPBFEntitiesPerBlock)
		}
		if tags > maxPBFTagsPerBlock {
			return fmt.Errorf("PBF block has more than %d tags", maxPBFTagsPerBlock)
		}
		return nil
	})
}

func validatePBFStringTable(data []byte) (int, error) {
	count, totalBytes := 0, 0
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
		if len(bytesValue) > maxPBFStringLength {
			return fmt.Errorf("PBF string table entry exceeds %d bytes", maxPBFStringLength)
		}
		totalBytes += len(bytesValue)
		if totalBytes > maxPBFStringTableBytes {
			return fmt.Errorf("PBF string table exceeds %d bytes", maxPBFStringTableBytes)
		}
		count++
		if count > maxPBFStringTableEntries {
			return fmt.Errorf("PBF string table has more than %d entries", maxPBFStringTableEntries)
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return count, nil
}

func validatePBFPrimitiveGroup(data []byte, stringCount, maxWayNodes int) (int, int, error) {
	denseSeen := false
	entities, tags := 0, 0
	err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 1:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid non-dense PBF nodes")
			}
			return fmt.Errorf("non-dense PBF nodes are unsupported")
		case 2:
			if fieldType != protowire.BytesType || denseSeen {
				return fmt.Errorf("invalid or repeated dense PBF nodes")
			}
			denseSeen = true
			entityCount, tagCount, err := validatePBFDenseNodes(bytesValue, stringCount)
			entities += entityCount
			tags += tagCount
			return err
		case 3:
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid PBF way")
			}
			entities++
			count, err := validatePBFWay(bytesValue, stringCount, maxWayNodes)
			tags += count
			return err
		case 4, 5:
			// Both decode passes skip relations and changesets as opaque bytes.
			if fieldType != protowire.BytesType {
				return fmt.Errorf("invalid PBF primitive group entity")
			}
			entities++
		}
		if entities > maxPBFEntitiesPerBlock {
			return fmt.Errorf("PBF primitive group has more than %d entities", maxPBFEntitiesPerBlock)
		}
		return nil
	})
	return entities, tags, err
}

func validatePBFDenseNodes(data []byte, stringCount int) (int, int, error) {
	var idsData, infoData, latsData, lonsData, tagsData []byte
	idsSeen, infoSeen, latsSeen, lonsSeen, tagsSeen := false, false, false, false, false
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 1:
			return capturePBFBytesField("dense node IDs", fieldType, bytesValue, &idsData, &idsSeen)
		case 5:
			return capturePBFBytesField("dense node info", fieldType, bytesValue, &infoData, &infoSeen)
		case 8:
			return capturePBFBytesField("dense node latitudes", fieldType, bytesValue, &latsData, &latsSeen)
		case 9:
			return capturePBFBytesField("dense node longitudes", fieldType, bytesValue, &lonsData, &lonsSeen)
		case 10:
			return capturePBFBytesField("dense node tags", fieldType, bytesValue, &tagsData, &tagsSeen)
		default:
			return nil
		}
	}); err != nil {
		return 0, 0, err
	}
	ids, err := countPackedPBFVarints(idsData)
	if err != nil {
		return 0, 0, err
	}
	lats, err := countPackedPBFVarints(latsData)
	if err != nil {
		return 0, 0, err
	}
	lons, err := countPackedPBFVarints(lonsData)
	if err != nil {
		return 0, 0, err
	}
	if !idsSeen || !latsSeen || !lonsSeen || ids != lats || ids != lons {
		return 0, 0, fmt.Errorf("dense PBF node columns have mismatched lengths")
	}
	if ids > maxPBFEntitiesPerBlock {
		return 0, 0, fmt.Errorf("dense PBF group has %d nodes, limit %d", ids, maxPBFEntitiesPerBlock)
	}
	if infoSeen {
		if err := validatePBFDenseInfo(infoData, ids, stringCount); err != nil {
			return 0, 0, err
		}
	}
	tags := 0
	if tagsSeen {
		tags, err = validatePBFDenseTags(tagsData, ids, stringCount)
		if err != nil {
			return 0, 0, err
		}
	}
	return ids, tags, nil
}

func validatePBFDenseInfo(data []byte, nodeCount, stringCount int) error {
	var seen [7]bool
	return eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		if number < 1 || number > 6 {
			return nil
		}
		if fieldType != protowire.BytesType || seen[number] {
			return fmt.Errorf("invalid or repeated dense PBF info field %d", number)
		}
		seen[number] = true
		count := 0
		userID := int64(0)
		if err := eachPackedPBFVarint(bytesValue, func(value uint64) error {
			count++
			if number != 5 {
				return nil
			}
			delta := protowire.DecodeZigZag(value)
			if delta < math.MinInt32 || delta > math.MaxInt32 {
				return fmt.Errorf("dense PBF user string index delta is out of range")
			}
			userID += delta
			if userID < 0 || userID >= int64(stringCount) {
				return fmt.Errorf("dense PBF user string index %d is out of range", userID)
			}
			return nil
		}); err != nil {
			return err
		}
		if count != nodeCount {
			return fmt.Errorf("dense PBF info field %d has %d values, want %d", number, count, nodeCount)
		}
		return nil
	})
}

func validatePBFDenseTags(data []byte, nodeCount, stringCount int) (int, error) {
	delimiters, tags, total := 0, 0, 0
	expectingKey := true
	if err := eachPackedPBFVarint(data, func(value uint64) error {
		if value == 0 {
			if !expectingKey {
				return fmt.Errorf("dense PBF tag stream is malformed")
			}
			delimiters++
			tags = 0
			return nil
		}
		if value > math.MaxInt32 || value >= uint64(stringCount) {
			return fmt.Errorf("dense PBF tag string index %d is out of range", value)
		}
		expectingKey = !expectingKey
		if expectingKey {
			tags++
			total++
			if tags > maxPBFTagsPerEntity {
				return fmt.Errorf("dense PBF node has more than %d tags", maxPBFTagsPerEntity)
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if !expectingKey || delimiters != nodeCount {
		return 0, fmt.Errorf("dense PBF tag stream is malformed")
	}
	return total, nil
}

func validatePBFWay(data []byte, stringCount, maxWayNodes int) (int, error) {
	var keysData, valuesData, refsData, latsData, lonsData []byte
	idSeen, keysSeen, valuesSeen, infoSeen := false, false, false, false
	refsSeen, latsSeen, lonsSeen := false, false, false
	if err := eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, bytesValue []byte, _ uint64) error {
		switch number {
		case 1:
			if fieldType != protowire.VarintType || idSeen {
				return fmt.Errorf("invalid or repeated PBF way ID")
			}
			idSeen = true
			return nil
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
		return 0, err
	}
	keys, err := validatePBFStringIndexes(keysData, stringCount)
	if err != nil {
		return 0, err
	}
	values, err := validatePBFStringIndexes(valuesData, stringCount)
	if err != nil {
		return 0, err
	}
	if !idSeen {
		return 0, fmt.Errorf("PBF way has no ID")
	}
	if keysSeen != valuesSeen || keys != values {
		return 0, fmt.Errorf("PBF way tag columns have mismatched lengths")
	}
	if keys > maxPBFTagsPerEntity {
		return 0, fmt.Errorf("PBF way has %d tags, limit %d", keys, maxPBFTagsPerEntity)
	}
	refs, err := countPackedPBFVarints(refsData)
	if err != nil {
		return 0, err
	}
	if refs > maxWayNodes {
		return 0, fmt.Errorf("%w: way has %d nodes, limit %d", ErrWayNodeLimit, refs, maxWayNodes)
	}
	latCount, err := countPackedPBFVarints(latsData)
	if err != nil {
		return 0, err
	}
	lonCount, err := countPackedPBFVarints(lonsData)
	if err != nil {
		return 0, err
	}
	if latsSeen && latCount != refs || lonsSeen && lonCount != refs {
		return 0, fmt.Errorf("PBF way coordinate columns have mismatched lengths")
	}
	return keys, nil
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
	var seen [7]bool
	return eachPBFField(data, func(number protowire.Number, fieldType protowire.Type, _ []byte, varintValue uint64) error {
		if number < 1 || number > 6 {
			return nil
		}
		if fieldType != protowire.VarintType || seen[number] {
			return fmt.Errorf("invalid or repeated PBF info field %d", number)
		}
		seen[number] = true
		if number == 5 && varintValue >= uint64(stringCount) {
			return fmt.Errorf("PBF user string index is out of range")
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
		if fieldType == protowire.StartGroupType || fieldType == protowire.EndGroupType {
			return fmt.Errorf("PBF protobuf groups are unsupported")
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
