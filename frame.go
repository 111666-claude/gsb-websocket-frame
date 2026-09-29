// Package wsframe 编解码 WebSocket 帧（RFC 6455 子集）。
package wsframe

import "errors"

var (
	// ErrShort 表示数据不完整。
	ErrShort = errors.New("wsframe: 数据不完整")
	// ErrUnmasked 表示客户端帧没有掩码（协议错误码 1002）。
	ErrUnmasked = errors.New("wsframe: 客户端帧必须加掩码（1002）")
	// ErrControl 表示控制帧不合法（协议错误码 1002）。
	ErrControl = errors.New("wsframe: 控制帧不合法（1002）")
	// ErrOpcode 表示不认识的 opcode（协议错误码 1002）。
	ErrOpcode = errors.New("wsframe: 不认识的 opcode（1002）")
	// ErrRSV 表示 RSV1/2/3 非 0（协议错误码 1002）。
	ErrRSV = errors.New("wsframe: RSV 位必须为 0（1002）")
	// ErrLength 表示长度字段不合法（协议错误码 1002）。
	ErrLength = errors.New("wsframe: 长度字段不合法（1002）")
	// ErrFragment 表示分片序列不合法（协议错误码 1002）。
	ErrFragment = errors.New("wsframe: 分片序列不合法（1002）")
	// ErrUTF8 表示文本消息不是合法 UTF-8（协议错误码 1007）。
	ErrUTF8 = errors.New("wsframe: 文本消息不是合法 UTF-8（1007）")
)

// Frame 是一帧。
type Frame struct {
	Fin     bool
	Opcode  byte
	Masked  bool
	Payload []byte
}

// Opcodes
const (
	Continuation = 0x0
	Text         = 0x1
	Binary       = 0x2
	Close        = 0x8
	Ping         = 0x9
	Pong         = 0xA
)

// IsControl 判断是不是控制帧。
func IsControl(opcode byte) bool {
	return opcode >= Close
}

// maskKey 是编码客户端帧时使用的固定掩码 key（固定以保证编码确定性）。
var maskKey = [4]byte{0x12, 0x34, 0x56, 0x78}

// Parse 解析一整帧（不强制掩码；客户端帧的严格解析见 Parser）。
func Parse(data []byte) (Frame, error) {
	frame, _, err := parseFrame(data, false)
	return frame, err
}

// parseFrame 从 data 头部解析一帧，返回帧与消费的字节数。
// 数据不足返回 ErrShort；协议错误返回带错误码的哨兵错误。
func parseFrame(data []byte, requireMask bool) (Frame, int, error) {
	if len(data) < 2 {
		return Frame{}, 0, ErrShort
	}
	if data[0]&0x70 != 0 {
		return Frame{}, 0, ErrRSV
	}
	frame := Frame{Fin: data[0]&0x80 != 0, Opcode: data[0] & 0x0F, Masked: data[1]&0x80 != 0}
	switch frame.Opcode {
	case Continuation, Text, Binary, Close, Ping, Pong:
	default:
		return Frame{}, 0, ErrOpcode
	}
	if requireMask && !frame.Masked {
		return Frame{}, 0, ErrUnmasked
	}
	length := uint64(data[1] & 0x7F)
	offset := 2
	switch length {
	case 126:
		if len(data) < offset+2 {
			return Frame{}, 0, ErrShort
		}
		length = uint64(data[offset])<<8 | uint64(data[offset+1])
		offset += 2
	case 127:
		if len(data) < offset+8 {
			return Frame{}, 0, ErrShort
		}
		if data[offset]&0x80 != 0 {
			return Frame{}, 0, ErrLength
		}
		length = 0
		for index := 0; index < 8; index++ {
			length = length<<8 | uint64(data[offset+index])
		}
		offset += 8
	}
	if IsControl(frame.Opcode) && (!frame.Fin || length > 125) {
		return Frame{}, 0, ErrControl
	}
	var key [4]byte
	if frame.Masked {
		if len(data) < offset+4 {
			return Frame{}, 0, ErrShort
		}
		copy(key[:], data[offset:offset+4])
		offset += 4
	}
	if uint64(len(data)-offset) < length {
		return Frame{}, 0, ErrShort
	}
	payload := make([]byte, length)
	copy(payload, data[offset:offset+int(length)])
	if frame.Masked {
		for index := range payload {
			payload[index] ^= key[index%4]
		}
	}
	frame.Payload = payload
	return frame, offset + int(length), nil
}

// Encode 编码一帧（masked 为 true 时按客户端帧编码，带掩码）。
// 同一帧重复编码结果逐字节相同。
func Encode(frame Frame, masked bool) []byte {
	out := []byte{}
	first := frame.Opcode & 0x0F
	if frame.Fin {
		first |= 0x80
	}
	out = append(out, first)
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	length := len(frame.Payload)
	switch {
	case length < 126:
		out = append(out, maskBit|byte(length))
	case length <= 0xFFFF:
		out = append(out, maskBit|126, byte(length>>8), byte(length))
	default:
		out = append(out, maskBit|127)
		for shift := 56; shift >= 0; shift -= 8 {
			out = append(out, byte(uint64(length)>>uint(shift)))
		}
	}
	if !masked {
		return append(out, frame.Payload...)
	}
	out = append(out, maskKey[:]...)
	for index, value := range frame.Payload {
		out = append(out, value^maskKey[index%4])
	}
	return out
}
