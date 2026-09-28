// Package wsframe 编解码 WebSocket 帧。
// 缺陷：解析时不还原掩码、也不拒绝未掩码的客户端帧、控制帧长度不校验。
package wsframe

import "errors"

var (
	// ErrShort 表示数据不完整。
	ErrShort = errors.New("wsframe: 数据不完整")
	// ErrUnmasked 表示客户端帧没有掩码（协议错误码 1002）。
	ErrUnmasked = errors.New("wsframe: 客户端帧必须加掩码（1002）")
	// ErrControl 表示控制帧不合法（协议错误码 1002）。
	ErrControl = errors.New("wsframe: 控制帧不合法（1002）")
	// ErrOpcode 表示不认识的 opcode。
	ErrOpcode = errors.New("wsframe: 不认识的 opcode")
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

// Parse 解析一帧。缺陷：掩码没还原、未掩码帧照收、控制帧长度不校验。
func Parse(data []byte) (Frame, error) {
	if len(data) < 2 {
		return Frame{}, ErrShort
	}
	frame := Frame{Fin: data[0]&0x80 != 0, Opcode: data[0] & 0x0F, Masked: data[1]&0x80 != 0}
	length := int64(data[1] & 0x7F)
	offset := 2
	switch length {
	case 126:
		if len(data) < offset+2 {
			return Frame{}, ErrShort
		}
		length = int64(data[offset])<<8 | int64(data[offset+1])
		offset += 2
	case 127:
		if len(data) < offset+8 {
			return Frame{}, ErrShort
		}
		length = 0
		for index := 0; index < 8; index++ {
			length = length<<8 | int64(data[offset+index])
		}
		offset += 8
	}
	if frame.Masked {
		if len(data) < offset+4 {
			return Frame{}, ErrShort
		}
		offset += 4 // 缺陷：读了掩码 key 却没有用它还原载荷
	}
	if int64(len(data)-offset) < length {
		return Frame{}, ErrShort
	}
	frame.Payload = append([]byte(nil), data[offset:offset+int(length)]...)
	return frame, nil
}

// Encode 编码一帧（客户端帧带掩码）。
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
	key := [4]byte{0x12, 0x34, 0x56, 0x78}
	out = append(out, key[:]...)
	for index, value := range frame.Payload {
		out = append(out, value^key[index%4])
	}
	return out
}
