package wsframe

import (
	"bytes"
	"math/rand"
	"testing"
	"time"
)

// refKey 与生产编码器使用同一固定掩码 key，便于逐字节比对。
var refKey = [4]byte{0x12, 0x34, 0x56, 0x78}

// refEncode 是测试内的参考编码器（独立实现）。
func refEncode(fin bool, opcode byte, payload []byte, masked bool) []byte {
	out := []byte{opcode}
	if fin {
		out[0] |= 0x80
	}
	var mask byte
	if masked {
		mask = 0x80
	}
	switch n := len(payload); {
	case n < 126:
		out = append(out, mask|byte(n))
	case n <= 0xFFFF:
		out = append(out, mask|126, byte(n>>8), byte(n))
	default:
		out = append(out, mask|127)
		for i := 7; i >= 0; i-- {
			out = append(out, byte(uint64(n)>>(8*uint(i))))
		}
	}
	if !masked {
		return append(out, payload...)
	}
	out = append(out, refKey[:]...)
	for i, b := range payload {
		out = append(out, b^refKey[i%4])
	}
	return out
}

// refDecode 是测试内的参考解码器（独立实现），返回消费字节数。
func refDecode(data []byte) (fin bool, opcode byte, payload []byte, consumed int, err error) {
	if len(data) < 2 {
		return false, 0, nil, 0, ErrShort
	}
	fin = data[0]&0x80 != 0
	opcode = data[0] & 0x0F
	masked := data[1]&0x80 != 0
	n := int(data[1] & 0x7F)
	off := 2
	switch n {
	case 126:
		if len(data) < 4 {
			return false, 0, nil, 0, ErrShort
		}
		n = int(data[2])<<8 | int(data[3])
		off = 4
	case 127:
		if len(data) < 10 {
			return false, 0, nil, 0, ErrShort
		}
		big := uint64(0)
		for i := 0; i < 8; i++ {
			big = big<<8 | uint64(data[2+i])
		}
		n = int(big)
		off = 10
	}
	var key []byte
	if masked {
		if len(data) < off+4 {
			return false, 0, nil, 0, ErrShort
		}
		key = data[off : off+4]
		off += 4
	}
	if len(data) < off+n {
		return false, 0, nil, 0, ErrShort
	}
	payload = make([]byte, n)
	for i := 0; i < n; i++ {
		b := data[off+i]
		if masked {
			b ^= key[i%4]
		}
		payload[i] = b
	}
	return fin, opcode, payload, off + n, nil
}

func TestParseUnmasksPayload(t *testing.T) {
	encoded := Encode(Frame{Fin: true, Opcode: Text, Payload: []byte("hello")}, true)
	frame, err := Parse(encoded)
	if err != nil {
		t.Fatalf("Parse 出错：%v", err)
	}
	if !frame.Masked {
		t.Fatal("应该识别出掩码位")
	}
	if !bytes.Equal(frame.Payload, []byte("hello")) {
		t.Fatalf("掩码没还原：%q", frame.Payload)
	}
}

func TestParserRejectsUnmaskedClientFrame(t *testing.T) {
	encoded := Encode(Frame{Fin: true, Opcode: Text, Payload: []byte("hello")}, false)
	parser := &Parser{RequireMask: true}
	if _, err := parser.Feed(encoded); err != ErrUnmasked {
		t.Fatalf("未掩码客户端帧应该报 ErrUnmasked：%v", err)
	}
}

func TestControlFrameConstraints(t *testing.T) {
	// 控制帧 FIN=0
	fragmented := []byte{0x09, 0x80, 0x12, 0x34, 0x56, 0x78}
	if _, err := Parse(fragmented); err != ErrControl {
		t.Fatalf("分片控制帧应该报 ErrControl：%v", err)
	}
	// 控制帧载荷 126 字节
	big := Encode(Frame{Fin: true, Opcode: Ping, Payload: bytes.Repeat([]byte("x"), 126)}, true)
	if _, err := Parse(big); err != ErrControl {
		t.Fatalf("超长控制帧应该报 ErrControl：%v", err)
	}
	// 125 字节的合法 ping 可以通过
	ok := Encode(Frame{Fin: true, Opcode: Ping, Payload: bytes.Repeat([]byte("x"), 125)}, true)
	if _, err := Parse(ok); err != nil {
		t.Fatalf("125 字节 ping 应该合法：%v", err)
	}
}

func TestRSVAndOpcodeChecks(t *testing.T) {
	if _, err := Parse([]byte{0xC1, 0x80, 0x12, 0x34, 0x56, 0x78}); err != ErrRSV {
		t.Fatalf("RSV 非 0 应该报 ErrRSV：%v", err)
	}
	if _, err := Parse([]byte{0x83, 0x80, 0x12, 0x34, 0x56, 0x78}); err != ErrOpcode {
		t.Fatalf("未知 opcode 应该报 ErrOpcode：%v", err)
	}
}

func TestTruncatedFrames(t *testing.T) {
	full := Encode(Frame{Fin: true, Opcode: Binary, Payload: bytes.Repeat([]byte("a"), 300)}, true)
	for _, n := range []int{1, 2, 3, 5, 8, len(full) - 1} {
		if _, err := Parse(full[:n]); err != ErrShort {
			t.Fatalf("截断到 %d 字节应该报 ErrShort：%v", n, err)
		}
	}
	// 长度字段声称 300，实际只给 10 个载荷字节
	liar := append([]byte{0x82, 0xFE, 0x01, 0x2C, 0x12, 0x34, 0x56, 0x78}, bytes.Repeat([]byte("a"), 10)...)
	if _, err := Parse(liar); err != ErrShort {
		t.Fatalf("长度与实际不符应该报 ErrShort：%v", err)
	}
}

func TestFragmentationReassembly(t *testing.T) {
	parser := &Parser{RequireMask: true}
	stream := [][]byte{
		Encode(Frame{Fin: false, Opcode: Text, Payload: []byte("你好，")}, true),
		Encode(Frame{Fin: true, Opcode: Ping, Payload: []byte("?")}, true), // 控制帧插在分片中间
		Encode(Frame{Fin: false, Opcode: Continuation, Payload: []byte("世")}, true),
		Encode(Frame{Fin: true, Opcode: Continuation, Payload: []byte("界")}, true),
	}
	var messages []Message
	for _, chunk := range stream {
		batch, err := parser.Feed(chunk)
		if err != nil {
			t.Fatalf("Feed 出错：%v", err)
		}
		messages = append(messages, batch...)
	}
	if len(messages) != 2 {
		t.Fatalf("应该重组出 2 条消息（ping + 文本）：%d", len(messages))
	}
	if messages[0].Opcode != Ping || string(messages[0].Payload) != "?" {
		t.Fatalf("控制帧应该单独上抛：%+v", messages[0])
	}
	if messages[1].Opcode != Text || string(messages[1].Payload) != "你好，世界" {
		t.Fatalf("分片重组不对：%q", messages[1].Payload)
	}
	if parser.Frames != 4 {
		t.Fatalf("帧数不对：%d", parser.Frames)
	}
}

func TestFragmentProtocolErrors(t *testing.T) {
	// 分片中间出现新的文本帧
	parser := &Parser{RequireMask: true}
	if _, err := parser.Feed(Encode(Frame{Fin: false, Opcode: Text, Payload: []byte("a")}, true)); err != nil {
		t.Fatalf("首帧不应该报错：%v", err)
	}
	if _, err := parser.Feed(Encode(Frame{Fin: true, Opcode: Binary, Payload: []byte("b")}, true)); err != ErrFragment {
		t.Fatalf("分片中间的新数据帧应该报 ErrFragment：%v", err)
	}
	// 没有首帧直接 Continuation
	parser = &Parser{RequireMask: true}
	if _, err := parser.Feed(Encode(Frame{Fin: true, Opcode: Continuation, Payload: []byte("x")}, true)); err != ErrFragment {
		t.Fatalf("裸 Continuation 应该报 ErrFragment：%v", err)
	}
}

func TestUTF8Validation(t *testing.T) {
	bad := []byte{0xFF, 0xFE}
	// 单帧文本
	parser := &Parser{RequireMask: true}
	if _, err := parser.Feed(Encode(Frame{Fin: true, Opcode: Text, Payload: bad}, true)); err != ErrUTF8 {
		t.Fatalf("非法 UTF-8 文本应该报 ErrUTF8：%v", err)
	}
	// 分片拼完才非法
	parser = &Parser{RequireMask: true}
	if _, err := parser.Feed(Encode(Frame{Fin: false, Opcode: Text, Payload: []byte{0xE4, 0xBD}}, true)); err != nil {
		t.Fatalf("分片中间不校验：%v", err)
	}
	if _, err := parser.Feed(Encode(Frame{Fin: true, Opcode: Continuation, Payload: []byte{0xFF}}, true)); err != ErrUTF8 {
		t.Fatalf("分片拼完非法应该报 ErrUTF8：%v", err)
	}
	// 二进制不校验
	parser = &Parser{RequireMask: true}
	messages, err := parser.Feed(Encode(Frame{Fin: true, Opcode: Binary, Payload: bad}, true))
	if err != nil || len(messages) != 1 {
		t.Fatalf("二进制不应该校验 UTF-8：%v %v", messages, err)
	}
}

func TestIncrementalFeedAcrossChunks(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	var stream []byte
	var want []Message
	// 一条分片文本 + 一条二进制 + 一个 ping，三种长度分支都覆盖
	parts := [][]byte{
		Encode(Frame{Fin: false, Opcode: Text, Payload: bytes.Repeat([]byte("中"), 100)}, true),
		Encode(Frame{Fin: true, Opcode: Continuation, Payload: bytes.Repeat([]byte("文"), 200)}, true),
		Encode(Frame{Fin: true, Opcode: Binary, Payload: rngBytes(rng, 70000)}, true),
		Encode(Frame{Fin: true, Opcode: Ping, Payload: []byte("ping")}, true),
	}
	for _, part := range parts {
		stream = append(stream, part...)
	}
	want = []Message{
		{Opcode: Text, Payload: append(bytes.Repeat([]byte("中"), 100), bytes.Repeat([]byte("文"), 200)...)},
		{Opcode: Binary, Payload: parts[2][len(parts[2])-70000:]},
	}
	// 还原二进制载荷（掩码过的，直接用参考解码器拿）
	_, _, binPayload, _, err := refDecode(parts[2])
	if err != nil {
		t.Fatalf("参考解码器出错：%v", err)
	}
	want[1].Payload = binPayload
	want = append(want, Message{Opcode: Ping, Payload: []byte("ping")})

	// 用随机小块（1~7 字节）喂，覆盖半个帧头/半个 mask key/半个载荷
	parser := &Parser{RequireMask: true}
	var got []Message
	for offset := 0; offset < len(stream); {
		n := 1 + rng.Intn(7)
		if offset+n > len(stream) {
			n = len(stream) - offset
		}
		batch, err := parser.Feed(stream[offset : offset+n])
		if err != nil {
			t.Fatalf("Feed 出错：%v", err)
		}
		got = append(got, batch...)
		offset += n
	}
	if len(got) != len(want) {
		t.Fatalf("消息数不对：got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Opcode != want[i].Opcode || !bytes.Equal(got[i].Payload, want[i].Payload) {
			t.Fatalf("消息 %d 不对：opcode=%d 载荷长度=%d", i, got[i].Opcode, len(got[i].Payload))
		}
	}
}

func TestEncodeDeterministic(t *testing.T) {
	frame := Frame{Fin: true, Opcode: Text, Payload: []byte("deterministic")}
	first := Encode(frame, true)
	second := Encode(frame, true)
	if !bytes.Equal(first, second) {
		t.Fatal("同一帧重复编码应该逐字节相同")
	}
}

func TestRoundtripAgainstReference(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	lengths := []int{0, 1, 5, 125, 126, 127, 200, 65535, 65536, 70000}
	opcodes := []byte{Text, Binary, Close, Ping, Pong}
	for i := 0; i < 200; i++ {
		opcode := opcodes[rng.Intn(len(opcodes))]
		n := lengths[rng.Intn(len(lengths))]
		if IsControl(opcode) && n > 125 {
			n = 125
		}
		payload := rngBytes(rng, n)
		if opcode == Text {
			payload = bytes.Repeat([]byte("字"), n/3) // 保证合法 UTF-8
		}
		masked := rng.Intn(2) == 0
		frame := Frame{Fin: true, Opcode: opcode, Payload: payload}

		// 编码与参考实现逐字节相同
		mine := Encode(frame, masked)
		ref := refEncode(true, opcode, payload, masked)
		if !bytes.Equal(mine, ref) {
			t.Fatalf("第 %d 帧编码与参考不一致", i)
		}
		// Parse(Encode(frame)) 载荷逐字节相同
		parsed, err := Parse(mine)
		if err != nil {
			t.Fatalf("第 %d 帧 Parse 出错：%v", i, err)
		}
		if parsed.Fin != frame.Fin || parsed.Opcode != frame.Opcode || parsed.Masked != masked ||
			!bytes.Equal(parsed.Payload, payload) {
			t.Fatalf("第 %d 帧 roundtrip 不一致", i)
		}
		// 参考解码器解我的编码也要一致
		refFin, refOpcode, refPayload, consumed, err := refDecode(mine)
		if err != nil || consumed != len(mine) {
			t.Fatalf("第 %d 帧参考解码出错：%v", i, err)
		}
		if refFin != frame.Fin || refOpcode != opcode || !bytes.Equal(refPayload, payload) {
			t.Fatalf("第 %d 帧与参考解码不一致", i)
		}
	}
}

func TestLargeFragmentedStream(t *testing.T) {
	const total = 10 << 20 // 10 MB
	const frag = 64 << 10  // 每片 64 KB
	rng := rand.New(rand.NewSource(7))

	var stream bytes.Buffer
	remaining := total
	first := true
	for remaining > 0 {
		n := frag
		if n > remaining {
			n = remaining
		}
		remaining -= n
		opcode := byte(Continuation)
		if first {
			opcode = Binary
			first = false
		}
		stream.Write(Encode(Frame{Fin: remaining == 0, Opcode: opcode, Payload: rngBytes(rng, n)}, true))
	}

	parser := &Parser{RequireMask: true}
	data := stream.Bytes()
	start := time.Now()
	messages := 0
	got := 0
	for offset := 0; offset < len(data); offset += 4096 {
		end := offset + 4096
		if end > len(data) {
			end = len(data)
		}
		batch, err := parser.Feed(data[offset:end])
		if err != nil {
			t.Fatalf("Feed 出错：%v", err)
		}
		for _, message := range batch {
			messages++
			got += len(message.Payload)
		}
	}
	elapsed := time.Since(start)
	if messages != 1 || got != total {
		t.Fatalf("应该重组出 1 条 10 MB 消息：messages=%d bytes=%d", messages, got)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("10 MB 分片流耗时 %v，超过 2 秒", elapsed)
	}
	t.Logf("10 MB 分片流按 4 KB 块喂入耗时 %v", elapsed)
}

func rngBytes(rng *rand.Rand, n int) []byte {
	out := make([]byte, n)
	rng.Read(out)
	return out
}
