# websocket-frame

WebSocket 帧编解码（RFC 6455 子集），只用 Go 标准库。

```
go test ./...
go run ./cmd/websocket-frame --payload hello --masked
go run ./cmd/websocket-frame --hex 8185123456787a513a147d
go run ./cmd/websocket-frame --stream < frames.bin
```

## 口径（README 为准）

- **掩码必须还原**：客户端帧带 4 字节 mask key，载荷每个字节 `payload[i] ^= key[i%4]`；
  编码客户端帧（`--masked`）时施加同一套掩码。
- **客户端帧必须带掩码**：`--hex 810568656c6c6f`（0x81 0x05 表示未掩码、长度 5）报
  `客户端帧必须加掩码（1002）`。
- **控制帧约束**：Close/Ping/Pong 的载荷 ≤ 125 字节且必须 `FIN=1`，否则报 1002。
- **opcode 白名单**：只支持 0/1/2/8/9/10，其它报 1002；RSV1/2/3 必须为 0，否则报 1002。
- **长度编码**：`< 126` 用 7 位；`126` 后跟 2 字节大端；`127` 后跟 8 字节大端（最高位必须为 0）。
  截断、长度与实际不符报 `ErrShort`。
- **分片重组**：`opcode=1/2 + 若干 Continuation + FIN` 拼成一条消息；控制帧可插在分片中间，
  单独上抛；分片中间出现新的文本/二进制帧报 1002；裸 Continuation 报 1002。
- **UTF-8 校验**：文本消息（含分片拼完的）必须是合法 UTF-8，否则报 1007；二进制不校验。
- **增量解析**：`Parser.Feed` 跨调用保存状态，半个帧头、半个 mask key、半个载荷都能跨块；
  内存 O(单帧 + 当前消息)。
- **不变量**：`Parse(Encode(frame, masked))` 的载荷逐字节相同；同一帧重复编码逐字节相同；
  随机 200 帧与参考编解码比对差异为 0。
- **规模**：10 MB 分片流按 4 KB 块喂入，2 秒内完成（实测约 0.05 s）。

## 命令行

```
--payload 文本    编码一帧文本并回读校验
--masked          按客户端帧编码（带掩码）
--hex 十六进制    按客户端帧（必须带掩码）解析一段帧
--stream          从标准输入按 4 KB 块增量解析
```

## 输出

```
bytes=11 hex=8185123456787a513a147d
payload=hello roundtrip=true
messages=1 frames=1 errors=0
```

协议错误输出带错误码，例如：

```
error=客户端帧必须加掩码（1002）
messages=0 frames=0 errors=1
```

## 目录

```
frame.go                 帧编解码
parser.go                增量解析与分片重组
cmd/websocket-frame      命令行入口
frame_test.go            帧编解码用例
parser_test.go           解析器、对照与性能用例
```
