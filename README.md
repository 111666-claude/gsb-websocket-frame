# websocket-frame

WebSocket 帧编解码（RFC 6455 子集），只用 Go 标准库。

```
go test ./...
go run ./cmd/websocket-frame --payload hello --masked
go run ./cmd/websocket-frame --hex 818568656c6c6f
```

## 口径（README 为准）

- **掩码必须还原**：客户端帧带 4 字节 mask key，载荷每个字节要 `payload[i] ^= key[i%4]`。
  `--payload hello --masked` 按口径是 `payload=hello roundtrip=true`，现在解析时读了 key 却没用，
  给 `payload=… roundtrip=false`。
- **客户端帧必须带掩码**：`--hex 818568656c6c6f`（0x81 0x85 表示未掩码、长度 5）按口径要报
  `客户端帧必须加掩码（1002）`，现在照收。
- **控制帧约束**：Close/Ping/Pong 的载荷 ≤ 125 字节且必须 `FIN=1`，否则报 1002；
  只支持 opcode 0/1/2/8/9/10，其它报错。
- 长度编码：`< 126` 用 7 位；`126` 后跟 2 字节大端；`127` 后跟 8 字节大端。
- 不变量：`Parse(Encode(frame, masked))` 得到的载荷与服务端帧（不带掩码）都要与原文逐字节相同；
  同一帧重复编码逐字节相同；截断的数据要报 `ErrShort`。
- 规模：10 MB 载荷的分帧要单遍处理，内存 O(载荷)；不许先把整个流拼成一个字符串再切。

## 现在的行为

- `Parse` 读了掩码 key 但没拿它异或载荷：带掩码的帧解出来是乱码。
- 未掩码的帧也照收，不返回 1002。
- 控制帧长度、RSV 位、opcode 白名单都不检查。

## 计划新增的能力

```
--payload 载荷    编码一帧文本
--masked          按客户端帧编码（带掩码）
--hex 十六进制    解析一段帧
```

## 输出

```
bytes=11 hex=8185123456787a5e591b
payload=hello roundtrip=true
```

## 目录

```
frame.go                 帧编解码
cmd/websocket-frame      命令行入口
frame_test.go            go test 用例
```
