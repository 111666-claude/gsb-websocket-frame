package wsframe

import "unicode/utf8"

// Message 是一条重组完成的消息；控制帧也作为独立消息上抛。
type Message struct {
	Opcode  byte // Text / Binary / Close / Ping / Pong
	Payload []byte
}

// Parser 是增量解析器：跨 Feed 调用保存状态（半个帧头、半个 mask key、
// 半个载荷都可以），并把分片帧重组成完整消息。内存占用 O(单帧 + 当前消息)。
type Parser struct {
	// RequireMask 为 true 时按服务端视角解析：客户端帧必须带掩码，否则报 1002。
	RequireMask bool

	// Frames 是已成功解析的帧数（含控制帧与分片）。
	Frames int

	buf       []byte // 尚未构成完整帧的残留字节
	inMsg     bool   // 是否处于一条分片消息中间
	msgOpcode byte   // 分片消息首帧的 opcode（Text 或 Binary）
	msg       []byte // 分片消息的重组缓冲
	failed    bool
	err       error
}

// Feed 喂入一段数据，返回本轮重组完成的消息（可能为零或多条）。
// 返回协议错误后解析器进入失败状态，后续 Feed 直接返回同一错误。
func (p *Parser) Feed(data []byte) ([]Message, error) {
	if p.failed {
		return nil, p.err
	}
	p.buf = append(p.buf, data...)
	var messages []Message
	for len(p.buf) > 0 {
		frame, consumed, err := parseFrame(p.buf, p.RequireMask)
		if err == ErrShort {
			break // 等更多数据
		}
		if err != nil {
			return messages, p.fail(err)
		}
		p.Frames++
		p.buf = p.buf[consumed:]
		if len(p.buf) == 0 {
			p.buf = p.buf[:0] // 复位偏移，避免底层数组只往前爬
		}
		message, complete, err := p.assemble(frame)
		if err != nil {
			return messages, p.fail(err)
		}
		if complete {
			messages = append(messages, message)
		}
	}
	return messages, nil
}

func (p *Parser) fail(err error) error {
	p.failed = true
	p.err = err
	return err
}

// assemble 把一帧并入分片状态机；控制帧直接作为独立消息返回。
func (p *Parser) assemble(frame Frame) (Message, bool, error) {
	if IsControl(frame.Opcode) {
		return Message{Opcode: frame.Opcode, Payload: frame.Payload}, true, nil
	}
	switch frame.Opcode {
	case Text, Binary:
		if p.inMsg {
			// 分片中间又出现新的数据帧：协议错误
			return Message{}, false, ErrFragment
		}
		if frame.Fin {
			return finish(frame.Opcode, frame.Payload)
		}
		p.inMsg = true
		p.msgOpcode = frame.Opcode
		p.msg = append(p.msg[:0], frame.Payload...)
		return Message{}, false, nil
	case Continuation:
		if !p.inMsg {
			return Message{}, false, ErrFragment
		}
		p.msg = append(p.msg, frame.Payload...)
		if !frame.Fin {
			return Message{}, false, nil
		}
		payload := p.msg
		p.msg = nil
		p.inMsg = false
		return finish(p.msgOpcode, payload)
	default:
		return Message{}, false, ErrOpcode // parseFrame 已拦截，不会走到
	}
}

// finish 校验并产出一条完整消息；文本消息必须是合法 UTF-8。
func finish(opcode byte, payload []byte) (Message, bool, error) {
	if opcode == Text && !utf8.Valid(payload) {
		return Message{}, false, ErrUTF8
	}
	return Message{Opcode: opcode, Payload: payload}, true, nil
}
