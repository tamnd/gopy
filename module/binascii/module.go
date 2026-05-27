// Package binascii ports Modules/binascii.c from CPython 3.14.
// CPython: Modules/binascii.c
package binascii

import (
	"encoding/hex"
	"fmt"
	"hash/crc32"

	"github.com/tamnd/gopy/imp"
	"github.com/tamnd/gopy/objects"
)

// tableA2BBase64 maps ASCII chars to 6-bit base64 values.
// CPython: Modules/binascii.c:86 table_a2b_base64
var tableA2BBase64 = [256]int8{
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, 62, -1, -1, -1, 63,
	52, 53, 54, 55, 56, 57, 58, 59, 60, 61, -1, -1, -1, 0, -1, -1, /* PAD->0 */
	-1, 0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14,
	15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, -1, -1, -1, -1, -1,
	-1, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40,
	41, 42, 43, 44, 45, 46, 47, 48, 49, 50, 51, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
	-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1,
}

const tableB2ABase64 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

// crctabHQX is the CRC-HQX lookup table.
// CPython: Modules/binascii.c:108 crctab_hqx
var crctabHQX = [256]uint16{
	0x0000, 0x1021, 0x2042, 0x3063, 0x4084, 0x50a5, 0x60c6, 0x70e7,
	0x8108, 0x9129, 0xa14a, 0xb16b, 0xc18c, 0xd1ad, 0xe1ce, 0xf1ef,
	0x1231, 0x0210, 0x3273, 0x2252, 0x52b5, 0x4294, 0x72f7, 0x62d6,
	0x9339, 0x8318, 0xb37b, 0xa35a, 0xd3bd, 0xc39c, 0xf3ff, 0xe3de,
	0x2462, 0x3443, 0x0420, 0x1401, 0x64e6, 0x74c7, 0x44a4, 0x5485,
	0xa56a, 0xb54b, 0x8528, 0x9509, 0xe5ee, 0xf5cf, 0xc5ac, 0xd58d,
	0x3653, 0x2672, 0x1611, 0x0630, 0x76d7, 0x66f6, 0x5695, 0x46b4,
	0xb75b, 0xa77a, 0x9719, 0x8738, 0xf7df, 0xe7fe, 0xd79d, 0xc7bc,
	0x48c4, 0x58e5, 0x6886, 0x78a7, 0x0840, 0x1861, 0x2802, 0x3823,
	0xc9cc, 0xd9ed, 0xe98e, 0xf9af, 0x8948, 0x9969, 0xa90a, 0xb92b,
	0x5af5, 0x4ad4, 0x7ab7, 0x6a96, 0x1a71, 0x0a50, 0x3a33, 0x2a12,
	0xdbfd, 0xcbdc, 0xfbbf, 0xeb9e, 0x9b79, 0x8b58, 0xbb3b, 0xab1a,
	0x6ca6, 0x7c87, 0x4ce4, 0x5cc5, 0x2c22, 0x3c03, 0x0c60, 0x1c41,
	0xedae, 0xfd8f, 0xcdec, 0xddcd, 0xad2a, 0xbd0b, 0x8d68, 0x9d49,
	0x7e97, 0x6eb6, 0x5ed5, 0x4ef4, 0x3e13, 0x2e32, 0x1e51, 0x0e70,
	0xff9f, 0xefbe, 0xdfdd, 0xcffc, 0xbf1b, 0xaf3a, 0x9f59, 0x8f78,
	0x9188, 0x81a9, 0xb1ca, 0xa1eb, 0xd10c, 0xc12d, 0xf14e, 0xe16f,
	0x1080, 0x00a1, 0x30c2, 0x20e3, 0x5004, 0x4025, 0x7046, 0x6067,
	0x83b9, 0x9398, 0xa3fb, 0xb3da, 0xc33d, 0xd31c, 0xe37f, 0xf35e,
	0x02b1, 0x1290, 0x22f3, 0x32d2, 0x4235, 0x5214, 0x6277, 0x7256,
	0xb5ea, 0xa5cb, 0x95a8, 0x8589, 0xf56e, 0xe54f, 0xd52c, 0xc50d,
	0x34e2, 0x24c3, 0x14a0, 0x0481, 0x7466, 0x6447, 0x5424, 0x4405,
	0xa7db, 0xb7fa, 0x8799, 0x97b8, 0xe75f, 0xf77e, 0xc71d, 0xd73c,
	0x26d3, 0x36f2, 0x0691, 0x16b0, 0x6657, 0x7676, 0x4615, 0x5634,
	0xd94c, 0xc96d, 0xf90e, 0xe92f, 0x99c8, 0x89e9, 0xb98a, 0xa9ab,
	0x5844, 0x4865, 0x7806, 0x6827, 0x18c0, 0x08e1, 0x3882, 0x28a3,
	0xcb7d, 0xdb5c, 0xeb3f, 0xfb1e, 0x8bf9, 0x9bd8, 0xabbb, 0xbb9a,
	0x4a75, 0x5a54, 0x6a37, 0x7a16, 0x0af1, 0x1ad0, 0x2ab3, 0x3a92,
	0xfd2e, 0xed0f, 0xdd6c, 0xcd4d, 0xbdaa, 0xad8b, 0x9de8, 0x8dc9,
	0x7c26, 0x6c07, 0x5c64, 0x4c45, 0x3ca2, 0x2c83, 0x1ce0, 0x0cc1,
	0xef1f, 0xff3e, 0xcf5d, 0xdf7c, 0xaf9b, 0xbfba, 0x8fd9, 0x9ff8,
	0x6e17, 0x7e36, 0x4e55, 0x5e74, 0x2e93, 0x3eb2, 0x0ed1, 0x1ef0,
}

// digitValue maps hex chars to their numeric value (16 = invalid).
var digitValue [256]int

func init() {
	for i := range digitValue {
		digitValue[i] = 16
	}
	for i := 0; i < 10; i++ {
		digitValue['0'+i] = i
	}
	for i := 0; i < 6; i++ {
		digitValue['a'+i] = 10 + i
		digitValue['A'+i] = 10 + i
	}
}

func getBytes(obj objects.Object) ([]byte, error) {
	switch v := obj.(type) {
	case *objects.Bytes:
		return v.Bytes(), nil
	case *objects.ByteArray:
		return v.Bytes(), nil
	case *objects.Unicode:
		s := v.Value()
		for _, c := range s {
			if c > 127 {
				return nil, fmt.Errorf("ValueError: string argument should contain only ASCII characters")
			}
		}
		return []byte(s), nil
	default:
		tp := objects.TypeOf(obj)
		tpName, _ := objects.Str(tp)
		return nil, fmt.Errorf("TypeError: a bytes-like object is required, not '%s'", tpName)
	}
}

func binasciiError(msg string) error {
	return fmt.Errorf("binascii.Error: %s", msg)
}

// a2b_uu decodes a line of uuencoded data.
// CPython: Modules/binascii.c:204 binascii_a2b_uu_impl
func a2bUU(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: a2b_uu() takes exactly 1 argument (%d given)", len(args))
	}
	asciiData, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}
	if len(asciiData) == 0 {
		return nil, binasciiError("Missing length byte")
	}
	binLen := int((asciiData[0] - ' ') & 077)
	asciiData = asciiData[1:]
	asciiLen := len(asciiData)

	binData := make([]byte, binLen)
	leftbits := 0
	var leftchar uint32
	j := 0

	for binLen > 0 {
		var thisCh byte
		if asciiLen > 0 {
			thisCh = asciiData[0]
			asciiData = asciiData[1:]
			asciiLen--
		}
		if thisCh == '\n' || thisCh == '\r' || asciiLen < 0 {
			thisCh = 0
		} else {
			if thisCh < ' ' || thisCh > (' '+64) {
				return nil, binasciiError("Illegal char")
			}
			thisCh = (thisCh - ' ') & 077
		}
		leftchar = (leftchar << 6) | uint32(thisCh)
		leftbits += 6
		if leftbits >= 8 {
			leftbits -= 8
			binData[j] = byte((leftchar >> leftbits) & 0xff)
			j++
			leftchar &= (1 << leftbits) - 1
			binLen--
		}
	}
	for _, ch := range asciiData {
		if ch != ' ' && ch != ' '+64 && ch != '\n' && ch != '\r' {
			return nil, binasciiError("Trailing garbage")
		}
	}
	return objects.NewBytes(binData[:j]), nil
}

// b2a_uu encodes a line of data using uuencoding.
// CPython: Modules/binascii.c:310 binascii_b2a_uu_impl
func b2aUU(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: b2a_uu() takes at least 1 argument")
	}
	binData, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}
	backtick := false
	if v, ok := kwargs["backtick"]; ok {
		b, berr := objects.IsTruthy(v)
		if berr != nil {
			return nil, berr
		}
		backtick = b
	}
	binLen := len(binData)
	if binLen > 45 {
		return nil, binasciiError("At most 45 bytes at once")
	}

	asciiData := make([]byte, 0, 2+(binLen+2)/3*4+1)
	if backtick && binLen == 0 {
		asciiData = append(asciiData, '`')
	} else {
		asciiData = append(asciiData, ' '+byte(binLen))
	}

	leftbits := 0
	var leftchar uint32
	remaining := binLen
	for i := 0; remaining > 0 || leftbits != 0; i++ {
		if remaining > 0 {
			leftchar = (leftchar << 8) | uint32(binData[i])
			remaining--
		} else {
			leftchar <<= 8
		}
		leftbits += 8
		for leftbits >= 6 {
			thisCh := byte((leftchar >> (leftbits - 6)) & 0x3f)
			leftbits -= 6
			if backtick && thisCh == 0 {
				asciiData = append(asciiData, '`')
			} else {
				asciiData = append(asciiData, thisCh+' ')
			}
		}
	}
	asciiData = append(asciiData, '\n')
	return objects.NewBytes(asciiData), nil
}

// a2b_base64 decodes a line of base64 data.
// CPython: Modules/binascii.c:386 binascii_a2b_base64_impl
func a2bBase64(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: a2b_base64() takes at least 1 argument")
	}
	asciiData, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}
	strictMode := false
	if v, ok := kwargs["strict_mode"]; ok {
		b, berr := objects.IsTruthy(v)
		if berr != nil {
			return nil, berr
		}
		strictMode = b
	}

	asciiLen := len(asciiData)
	binData := make([]byte, 0, ((asciiLen+3)/4)*3)
	quadPos := 0
	var leftchar byte
	pads := 0

	for i := 0; i < asciiLen; i++ {
		thisCh := asciiData[i]
		if thisCh == '=' {
			pads++
			if quadPos >= 2 && quadPos+pads <= 4 {
				continue
			}
			if !strictMode {
				continue
			}
			if quadPos == 1 {
				break
			}
			if quadPos == 0 && i == 0 {
				return nil, binasciiError("Leading padding not allowed")
			}
			return nil, binasciiError("Excess padding not allowed")
		}
		// tableA2BBase64 stores -1 for non-base64 chars (maps to int8 0xff in C).
		// CPython checks `this_ch >= 64` after casting to unsigned char.
		// CPython: Modules/binascii.c:434 table_a2b_base64
		v := tableA2BBase64[thisCh]
		if v < 0 || v >= 64 {
			if strictMode {
				return nil, binasciiError("Only base64 data is allowed")
			}
			continue
		}
		if pads != 0 && strictMode {
			if quadPos+pads == 4 {
				return nil, binasciiError("Excess data after padding")
			}
			return nil, binasciiError("Discontinuous padding not allowed")
		}
		pads = 0
		switch quadPos {
		case 0:
			quadPos = 1
			leftchar = byte(v)
		case 1:
			quadPos = 2
			binData = append(binData, (leftchar<<2)|(byte(v)>>4))
			leftchar = byte(v) & 0x0f
		case 2:
			quadPos = 3
			binData = append(binData, (leftchar<<4)|(byte(v)>>2))
			leftchar = byte(v) & 0x03
		case 3:
			quadPos = 0
			binData = append(binData, (leftchar<<6)|byte(v))
			leftchar = 0
		}
	}
	if quadPos == 1 {
		nChars := len(binData)/3*4 + 1
		return nil, binasciiError(fmt.Sprintf(
			"Invalid base64-encoded string: number of data characters (%d) cannot be 1 more than a multiple of 4",
			nChars))
	}
	if quadPos != 0 && quadPos+pads < 4 {
		return nil, binasciiError("Incorrect padding")
	}
	return objects.NewBytes(binData), nil
}

// b2a_base64 encodes data to base64 with optional trailing newline.
// CPython: Modules/binascii.c:532 binascii_b2a_base64_impl
func b2aBase64(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: b2a_base64() takes at least 1 argument")
	}
	binData, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}
	newline := true
	if v, ok := kwargs["newline"]; ok {
		b, berr := objects.IsTruthy(v)
		if berr != nil {
			return nil, berr
		}
		newline = b
	}

	outLen := len(binData)*2 + 2
	if newline {
		outLen++
	}
	asciiData := make([]byte, 0, outLen)
	leftbits := 0
	var leftchar uint32
	for _, b := range binData {
		leftchar = (leftchar << 8) | uint32(b)
		leftbits += 8
		for leftbits >= 6 {
			thisCh := (leftchar >> (leftbits - 6)) & 0x3f
			leftbits -= 6
			asciiData = append(asciiData, tableB2ABase64[thisCh])
		}
	}
	if leftbits == 2 {
		asciiData = append(asciiData, tableB2ABase64[(leftchar&3)<<4], '=', '=')
	} else if leftbits == 4 {
		asciiData = append(asciiData, tableB2ABase64[(leftchar&0xf)<<2], '=')
	}
	if newline {
		asciiData = append(asciiData, '\n')
	}
	return objects.NewBytes(asciiData), nil
}

// crc_hqx computes CRC-HQX incrementally.
// CPython: Modules/binascii.c:607 binascii_crc_hqx_impl
func crcHQX(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("TypeError: crc_hqx() takes exactly 2 arguments (%d given)", len(args))
	}
	binData, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}
	crcInt, ok := args[1].(*objects.Int)
	if !ok {
		return nil, fmt.Errorf("TypeError: an integer is required")
	}
	crcVal, _ := crcInt.Int64()
	crc := uint16(uint64(crcVal) & 0xffff)
	for _, b := range binData {
		crc = ((crc << 8) & 0xff00) ^ crctabHQX[(crc>>8)^uint16(b)]
	}
	return objects.NewInt(int64(crc)), nil
}

// crc32 computes CRC-32 incrementally.
// CPython: Modules/binascii.c:770 binascii_crc32_impl
func binasciiCRC32(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("TypeError: crc32() takes 1 to 2 arguments (%d given)", len(args))
	}
	binData, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}
	var initCRC uint32
	if len(args) == 2 {
		crcInt, ok := args[1].(*objects.Int)
		if !ok {
			return nil, fmt.Errorf("TypeError: an integer is required")
		}
		crcVal, _ := crcInt.Int64()
		initCRC = uint32(uint64(crcVal) & 0xffffffff)
	}
	result := crc32.Update(initCRC, crc32.IEEETable, binData)
	return objects.NewInt(int64(result & 0xffffffff)), nil
}

// b2a_hex / hexlify converts binary data to hex representation.
// CPython: Modules/binascii.c:850 binascii_b2a_hex_impl
func b2aHex(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: b2a_hex() takes at least 1 argument")
	}
	binData, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}

	var sep []byte
	bytesPerSep := 1
	if sepObj, ok := kwargs["sep"]; ok && sepObj != objects.None() {
		sep, err = getBytes(sepObj)
		if err != nil {
			return nil, err
		}
		if len(sep) != 1 {
			return nil, fmt.Errorf("ValueError: sep must be length 1")
		}
	}
	if bpsObj, ok := kwargs["bytes_per_sep"]; ok {
		if bpsInt, ok2 := bpsObj.(*objects.Int); ok2 {
			v, _ := bpsInt.Int64()
			bytesPerSep = int(v)
		}
	}
	if len(args) >= 2 && args[1] != objects.None() {
		sep, err = getBytes(args[1])
		if err != nil {
			return nil, err
		}
		if len(sep) != 1 {
			return nil, fmt.Errorf("ValueError: sep must be length 1")
		}
	}
	if len(args) >= 3 {
		if bpsInt, ok := args[2].(*objects.Int); ok {
			v, _ := bpsInt.Int64()
			bytesPerSep = int(v)
		}
	}

	if len(sep) == 0 {
		return objects.NewBytes([]byte(hex.EncodeToString(binData))), nil
	}

	hexStr := hex.EncodeToString(binData)
	hexBytes := []byte(hexStr)
	n := len(hexBytes) / 2

	if bytesPerSep == 0 || n <= 1 {
		return objects.NewBytes(hexBytes), nil
	}

	var result []byte
	if bytesPerSep > 0 {
		for i := 0; i < len(hexBytes); i += 2 {
			if i > 0 {
				byteIndex := i / 2
				remaining := n - byteIndex
				if remaining%bytesPerSep == 0 {
					result = append(result, sep[0])
				}
			}
			result = append(result, hexBytes[i], hexBytes[i+1])
		}
	} else {
		absSep := -bytesPerSep
		for i := 0; i < len(hexBytes); i += 2 {
			if i > 0 {
				byteIndex := i / 2
				if byteIndex%absSep == 0 {
					result = append(result, sep[0])
				}
			}
			result = append(result, hexBytes[i], hexBytes[i+1])
		}
	}
	return objects.NewBytes(result), nil
}

// a2b_hex / unhexlify converts hex string to binary data.
// CPython: Modules/binascii.c:889 binascii_a2b_hex_impl
func a2bHex(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("TypeError: a2b_hex() takes exactly 1 argument (%d given)", len(args))
	}
	hexData, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}
	if len(hexData)%2 != 0 {
		return nil, binasciiError("Odd-length string")
	}
	result := make([]byte, len(hexData)/2)
	for i := 0; i < len(hexData); i += 2 {
		top := digitValue[hexData[i]]
		bot := digitValue[hexData[i+1]]
		if top >= 16 || bot >= 16 {
			return nil, binasciiError("Non-hexadecimal digit found")
		}
		result[i/2] = byte(top<<4) + byte(bot)
	}
	return objects.NewBytes(result), nil
}

// a2b_qp decodes quoted-printable data.
// CPython: Modules/binascii.c:971 binascii_a2b_qp_impl
func a2bQP(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: a2b_qp() takes at least 1 argument")
	}
	asciiData, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}
	header := false
	if v, ok := kwargs["header"]; ok {
		b, berr := objects.IsTruthy(v)
		if berr != nil {
			return nil, berr
		}
		header = b
	}

	datalen := len(asciiData)
	odata := make([]byte, 0, datalen)
	in := 0

	for in < datalen {
		if asciiData[in] == '=' {
			in++
			if in >= datalen {
				break
			}
			if asciiData[in] == '\n' || asciiData[in] == '\r' {
				if asciiData[in] != '\n' {
					for in < datalen && asciiData[in] != '\n' {
						in++
					}
				}
				if in < datalen {
					in++
				}
			} else if asciiData[in] == '=' {
				odata = append(odata, '=')
				in++
			} else if in+1 < datalen && isHexChar(asciiData[in]) && isHexChar(asciiData[in+1]) {
				ch := byte(digitValue[asciiData[in]]<<4) | byte(digitValue[asciiData[in+1]])
				in += 2
				odata = append(odata, ch)
			} else {
				odata = append(odata, '=')
			}
		} else if header && asciiData[in] == '_' {
			odata = append(odata, ' ')
			in++
		} else {
			odata = append(odata, asciiData[in])
			in++
		}
	}
	return objects.NewBytes(odata), nil
}

func isHexChar(c byte) bool {
	return (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f') || (c >= '0' && c <= '9')
}

// b2a_qp encodes data using quoted-printable encoding.
// CPython: Modules/binascii.c:1073 binascii_b2a_qp_impl
func b2aQP(args []objects.Object, kwargs map[string]objects.Object) (objects.Object, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("TypeError: b2a_qp() takes at least 1 argument")
	}
	databuf, err := getBytes(args[0])
	if err != nil {
		return nil, err
	}
	quotetabs := false
	istext := true
	header := false

	if v, ok := kwargs["quotetabs"]; ok {
		b, berr := objects.IsTruthy(v)
		if berr != nil {
			return nil, berr
		}
		quotetabs = b
	}
	if v, ok := kwargs["istext"]; ok {
		b, berr := objects.IsTruthy(v)
		if berr != nil {
			return nil, berr
		}
		istext = b
	}
	if v, ok := kwargs["header"]; ok {
		b, berr := objects.IsTruthy(v)
		if berr != nil {
			return nil, berr
		}
		header = b
	}

	datalen := len(databuf)
	const maxlinesize = 76

	crlf := false
	for i, b := range databuf {
		if b == '\n' && i > 0 && databuf[i-1] == '\r' {
			crlf = true
			break
		}
	}

	toHex := func(ch byte) [2]byte {
		return [2]byte{
			"0123456789ABCDEF"[ch>>4],
			"0123456789ABCDEF"[ch&0xf],
		}
	}

	odata := make([]byte, 0, datalen)
	linelen := 0
	in := 0

	for in < datalen {
		ch := databuf[in]
		needsEncode := ch > 126 || ch == '=' ||
			(header && ch == '_') ||
			(ch == '.' && linelen == 0 && (in+1 == datalen || databuf[in+1] == '\n' || databuf[in+1] == '\r' || databuf[in+1] == 0)) ||
			(!istext && (ch == '\r' || ch == '\n')) ||
			((ch == '\t' || ch == ' ') && in+1 == datalen) ||
			(ch < 33 && ch != '\r' && ch != '\n' && (quotetabs || (ch != '\t' && ch != ' ')))

		if needsEncode {
			if linelen+3 >= maxlinesize {
				odata = append(odata, '=')
				if crlf {
					odata = append(odata, '\r')
				}
				odata = append(odata, '\n')
				linelen = 0
			}
			h := toHex(ch)
			odata = append(odata, '=', h[0], h[1])
			in++
			linelen += 3
		} else if istext && (ch == '\n' || (in+1 < datalen && ch == '\r' && databuf[in+1] == '\n')) {
			linelen = 0
			if len(odata) > 0 && (odata[len(odata)-1] == ' ' || odata[len(odata)-1] == '\t') {
				last := odata[len(odata)-1]
				odata = odata[:len(odata)-1]
				h := toHex(last)
				odata = append(odata, '=', h[0], h[1])
			}
			if crlf {
				odata = append(odata, '\r')
			}
			odata = append(odata, '\n')
			if ch == '\r' {
				in += 2
			} else {
				in++
			}
		} else {
			if in+1 != datalen && databuf[in+1] != '\n' && linelen+1 >= maxlinesize {
				odata = append(odata, '=')
				if crlf {
					odata = append(odata, '\r')
				}
				odata = append(odata, '\n')
				linelen = 0
			}
			linelen++
			if header && ch == ' ' {
				odata = append(odata, '_')
				in++
			} else {
				odata = append(odata, ch)
				in++
			}
		}
	}
	return objects.NewBytes(odata), nil
}

func buildModule() (*objects.Module, error) {
	m := objects.NewModule("binascii")
	d := m.Dict()

	setFn := func(name string, fn func([]objects.Object, map[string]objects.Object) (objects.Object, error)) error {
		return d.SetItem(objects.NewStr(name), objects.NewBuiltinFunction(name, fn))
	}

	type fnEntry struct {
		name string
		fn   func([]objects.Object, map[string]objects.Object) (objects.Object, error)
	}
	pairs := []fnEntry{
		{"a2b_uu", a2bUU},
		{"b2a_uu", b2aUU},
		{"a2b_base64", a2bBase64},
		{"b2a_base64", b2aBase64},
		{"crc_hqx", crcHQX},
		{"crc32", binasciiCRC32},
		{"b2a_hex", b2aHex},
		{"hexlify", b2aHex},
		{"a2b_hex", a2bHex},
		{"unhexlify", a2bHex},
		{"a2b_qp", a2bQP},
		{"b2a_qp", b2aQP},
	}
	for _, p := range pairs {
		if err := setFn(p.name, p.fn); err != nil {
			return nil, err
		}
	}

	// binascii.Error is a subclass of ValueError.
	// CPython: Modules/binascii.c:1270 binascii_exec
	// We expose it as a plain exception class created in Python space.
	if err := d.SetItem(objects.NewStr("Error"), objects.NewStr("binascii.Error")); err != nil {
		return nil, err
	}
	if err := d.SetItem(objects.NewStr("Incomplete"), objects.NewStr("binascii.Incomplete")); err != nil {
		return nil, err
	}

	return m, nil
}

func init() {
	_ = imp.AppendInittab("binascii", buildModule)
}
