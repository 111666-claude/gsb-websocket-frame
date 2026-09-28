package wsframe

import (
	"bytes"
	"testing"
)

func TestEncodeShortFrame(t *testing.T) {
	encoded := Encode(Frame{Fin: true, Opcode: Text, Payload: []byte("hi")}, false)
	if encoded[0] != 0x81 || encoded[1] != 2 {
		t.Fatalf("短帧头部不对：%v", encoded[:2])
	}
}

func TestEncodeLongFrameUsesSixteenBits(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 200)
	encoded := Encode(Frame{Fin: true, Opcode: Binary, Payload: payload}, false)
	if encoded[1] != 126 {
		t.Fatalf("200 字节应该用 126 分支：%d", encoded[1])
	}
	if len(encoded) != 4+200 {
		t.Fatalf("编码长度不对：%d", len(encoded))
	}
}

func TestParseUnmaskedPayload(t *testing.T) {
	encoded := Encode(Frame{Fin: true, Opcode: Text, Payload: []byte("hello")}, false)
	frame, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse 出错：%v", err)
	}
	if !bytes.Equal(frame.Payload, []byte("hello")) {
		t.Fatalf("载荷不对：%q", frame.Payload)
	}
	if !frame.Fin || frame.Opcode != Text {
		t.Fatalf("头部解析不对：%+v", frame)
	}
}

func TestParseShortData(t *testing.T) {
	if _, err := Parse([]byte{0x81}); err != ErrShort {
		t.Fatalf("一个字节应该报 ErrShort：%v", err)
	}
}

func TestIsControl(t *testing.T) {
	if !IsControl(Close) || !IsControl(Ping) || IsControl(Text) {
		t.Fatal("控制帧判断不对")
	}
}
