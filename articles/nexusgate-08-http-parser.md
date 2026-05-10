---
title: "NexusGate HTTP 解析器：从 TCP 流到结构化请求"
slug: "nexusgate-08-http-parser"
summary: "深入 NexusGate 的 HTTP 解析器实现，详解 ParseRequest 的七步解析流程、chunked encoding 支持、请求头安全限制、WriteResponse 的 CRLF 注入防护以及错误响应的 JSON 格式化输出。"
category: "NexusGate"
tags: ["Go", "HTTP解析", "chunked编码", "安全防护", "CRLF注入"]
is_draft: false
---

# 08 | HTTP 解析器：从 TCP 流到结构化请求
> 「用 Go 构建网关」专栏第 8 篇。本文详解 HTTP 请求解析和响应写入的实现细节。

---

## 为什么自研 HTTP 解析器？

NexusGate 不使用 Go 标准库的 `http.ReadRequest`，而是自研解析器。原因有三：

| 维度 | http.ReadRequest | 自研 Parser |
|------|------------------|-------------|
| 依赖 | `net/http` 内部 API | 仅 `bufio` + `strconv` |
| 控制 | 无法限制头部行数/大小 | 可配置 maxHeaderBytes/maxHeaderLines |
| 安全 | 依赖 Go 版本的安全修复 | 可主动防御 CRLF 注入等攻击 |
| 扩展 | 难以添加自定义解析逻辑 | 可自由扩展 |

自研解析器的代价是**不支持 HTTP/2 和 HTTP/1.1 Keep-Alive**——当前版本是短连接模型。这是有意的简化，Keep-Alive 和 HTTP/2 将在后续版本实现。

## Parser 结构

```go
type Parser struct {
    maxHeaderBytes int64          // 最大头部大小，默认 1MB
    maxBodyBytes   int64          // 最大体大小，默认 10MB
    readTimeout    time.Duration  // 读取超时，默认 30s
}
```

## ParseRequest 七步解析

### 步骤 1：设置读超时

```go
conn.SetReadDeadline(time.Now().Add(p.readTimeout))
```

每个连接设置读超时，防止慢速客户端占用连接资源。

### 步骤 2：创建缓冲读取器

```go
reader := bufio.NewReaderSize(conn, 4096)
```

4KB 缓冲区，减少系统调用次数。对于大多数 HTTP 请求，4KB 足以容纳完整的请求行和头部。

### 步骤 3：解析请求行

```go
func parseRequestLine(line string) (method, path, proto string, err error) {
    parts := strings.Split(line, " ")
    if len(parts) != 3 {
        return "", "", "", fmt.Errorf("malformed request line")
    }
    return parts[0], parts[1], parts[2], nil
}
```

标准 HTTP/1.1 请求行格式：`METHOD PATH PROTOCOL`，如 `GET /api/users HTTP/1.1`。

### 步骤 4：协议校验

```go
if proto != "HTTP/1.0" && proto != "HTTP/1.1" {
    return nil, gateway.NewGatewayError(gateway.ErrBadRequest, "unsupported protocol", proto)
}
```

仅支持 HTTP/1.0 和 HTTP/1.1，拒绝 HTTP/2（PRI * HTTP/2.0）和其他协议。

### 步骤 5：解析请求头

```go
func readHeaders(reader *bufio.Reader, maxHeaderBytes int64) (http.Header, error) {
    headers := make(http.Header)
    var totalSize int64
    maxHeaderLines := 256

    for i := 0; i < maxHeaderLines; i++ {
        line, err := readLine(reader)
        if err != nil {
            return headers, err
        }
        if len(line) == 0 {
            break
        }
        totalSize += int64(len(line))
        if totalSize > maxHeaderBytes {
            return nil, fmt.Errorf("header too large")
        }

        key, value, ok := parseHeaderLine(line)
        if !ok {
            continue
        }
        headers.Add(key, value)
    }
    return headers, nil
}
```

**三重保护**：
1. **行数限制**：最多 256 行头部，防止头部炸弹攻击
2. **大小限制**：总大小不超过 `maxHeaderBytes`（1MB）
3. **解析容错**：格式错误的头部行被跳过（`continue`），不中断解析

### 步骤 6：解析请求体

```go
func readBody(reader *bufio.Reader, headers http.Header, maxBodyBytes int64) ([]byte, error) {
    if headers.Get("Transfer-Encoding") == "chunked" {
        return readChunkedBody(reader, maxBodyBytes)
    }

    contentLength := headers.Get("Content-Length")
    if contentLength == "" {
        return nil, nil
    }

    length, err := strconv.ParseInt(contentLength, 10, 64)
    if err != nil || length < 0 {
        return nil, fmt.Errorf("invalid content length")
    }
    if length > maxBodyBytes {
        return nil, fmt.Errorf("body too large")
    }

    body := make([]byte, length)
    _, err = io.ReadFull(reader, body)
    return body, err
}
```

**体大小校验**：`Content-Length` 必须为非负整数且不超过 `maxBodyBytes`（10MB），防止超大请求体导致 OOM。

### 步骤 7：提取元数据

```go
req := &gateway.Request{
    Method:      method,
    Path:        pathOnly,
    QueryString: queryString,
    Host:        host,
    Scheme:      scheme,
    Headers:     headers,
    Body:        body,
    RemoteAddr:  conn.RemoteAddr().String(),
    TenantID:    tenantID,
}
```

- **Path 与 QueryString 分离**：`/api/users?page=1` → Path=`/api/users`, QueryString=`page=1`
- **Host 提取**：优先取 `Host` 头，缺失取 `conn.LocalAddr()`
- **Scheme 判断**：根据 `X-Forwarded-Proto` 头判断原始协议
- **TenantID**：取 `X-Tenant-ID` 头，默认 `"default"`

## Chunked Encoding 支持

### RFC 7230 规范

```
chunked-body   = *chunk
                 last-chunk
                 trailer-part
                 CRLF

chunk          = chunk-size [ chunk-ext ] CRLF
                 chunk-data CRLF
chunk-size     = 1*HEXDIG
last-chunk     = 1*("0") [ chunk-ext ] CRLF
```

### 实现

```go
func readChunkedBody(reader *bufio.Reader, maxBytes int64) ([]byte, error) {
    var body []byte
    var total int64

    for {
        line, _ := readLine(reader)

        semicolonIdx := strings.IndexByte(line, ';')
        if semicolonIdx != -1 {
            line = line[:semicolonIdx]
        }
        line = strings.TrimSpace(line)

        chunkSize, err := strconv.ParseInt(line, 16, 64)
        if err != nil {
            return nil, fmt.Errorf("invalid chunk size: %s", line)
        }

        if chunkSize == 0 {
            readTrailerHeaders(reader)
            break
        }

        total += chunkSize
        if total > maxBytes {
            return nil, fmt.Errorf("chunked body too large")
        }

        chunk := make([]byte, chunkSize)
        if _, err := io.ReadFull(reader, chunk); err != nil {
            return nil, fmt.Errorf("reading chunk data: %w", err)
        }

        crlf := make([]byte, 2)
        if _, err := io.ReadFull(reader, crlf); err != nil {
            return nil, fmt.Errorf("reading chunk CRLF: %w", err)
        }
        if crlf[0] != '\r' || crlf[1] != '\n' {
            return nil, fmt.Errorf("missing CRLF after chunk data")
        }

        body = append(body, chunk...)
    }

    return body, nil
}
```

**关键实现细节**：

1. **chunk extension 忽略**：分号后的部分被截断（如 `1000;ext=val` → `1000`），网关不需要解析 extension
2. **十六进制解析**：`strconv.ParseInt(line, 16, 64)` 解析 chunk size
3. **CRLF 验证**：每个 chunk 数据后必须跟 `\r\n`，否则报错
4. **终止 chunk**：size=0 表示结束，之后读取 trailer headers
5. **总大小限制**：所有 chunk 的累计大小不超过 `maxBodyBytes`

### Trailer Headers

```go
func readTrailerHeaders(reader *bufio.Reader) {
    for {
        line, err := readLine(reader)
        if err != nil || len(line) == 0 {
            break
        }
    }
}
```

Trailer headers 被读取但丢弃——网关不转发 trailer 到后端。

## WriteResponse 响应写入

### 核心流程

```go
func WriteResponse(conn net.Conn, resp *gateway.Response) error {
    statusText := http.StatusText(resp.StatusCode)
    if statusText == "" {
        statusText = "Unknown"
    }

    if _, err := fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\n", resp.StatusCode, statusText); err != nil {
        return err
    }

    if resp.Body != nil && len(resp.Body) > 0 {
        resp.Headers.Set("Content-Length", strconv.Itoa(len(resp.Body)))
    }

    for key, values := range resp.Headers {
        for _, value := range values {
            if _, err := fmt.Fprintf(conn, "%s: %s\r\n",
                sanitizeHeader(key), sanitizeHeader(value)); err != nil {
                return err
            }
        }
    }

    if _, err := conn.Write([]byte("\r\n")); err != nil {
        return err
    }

    if resp.Body != nil && len(resp.Body) > 0 {
        if _, err := conn.Write(resp.Body); err != nil {
            return err
        }
    }

    return nil
}
```

**Content-Length 自动设置**：如果响应体非空，自动设置 `Content-Length` 头。这确保客户端能正确判断响应结束。

### CRLF 注入防护

```go
func sanitizeHeader(s string) string {
    s = strings.ReplaceAll(s, "\r", "")
    s = strings.ReplaceAll(s, "\n", "")
    return s
}
```

**HTTP 响应拆分攻击**：如果攻击者能控制响应头的值，注入 `\r\n` 可以伪造额外的响应头甚至响应体：

```
HTTP/1.1 200 OK\r\n
X-Custom: value\r\nInjected-Header: malicious\r\n\r\nFake Body
```

`sanitizeHeader` 去除所有 `\r` 和 `\n`，确保头部值不会被注入。

## WriteErrorResponse 错误响应

```go
func WriteErrorResponse(conn net.Conn, gwErr *gateway.GatewayError) {
    statusCode := gwErr.HTTPStatus()
    errorBody := map[string]interface{}{
        "code":    gwErr.Code,
        "message": gwErr.Message,
        "detail":  gwErr.Detail,
    }
    body, _ := json.Marshal(errorBody)

    resp := &gateway.Response{
        StatusCode: statusCode,
        Headers: http.Header{
            "Content-Type": []string{"application/json"},
        },
        Body: body,
    }
    WriteResponse(conn, resp)
}
```

**统一 JSON 错误格式**：

```json
{
  "code": 10004,
  "message": "rate limit exceeded",
  "detail": "too many requests for tenant: acme-corp"
}
```

客户端可以解析 `code` 做程序化处理，`message` 提供人类可读的描述，`detail` 包含调试信息。

## 安全防护总结

| 攻击类型 | 防护措施 | 位置 |
|----------|----------|------|
| 头部炸弹 | 256 行限制 + 1MB 大小限制 | readHeaders |
| 超大请求体 | 10MB 大小限制 | readBody |
| CRLF 注入 | sanitizeHeader 去除 \r\n | WriteResponse |
| 慢速攻击 | 30s 读超时 | ParseRequest |
| 无效协议 | 仅允许 HTTP/1.0/1.1 | ParseRequest |
| 无效 Content-Length | 非负整数校验 | readBody |

## 小结

NexusGate 的 HTTP 解析器通过自研实现获得了三个优势：

1. **安全可控**：主动限制头部行数/大小、体大小、CRLF 注入
2. **可配置**：maxHeaderBytes、maxBodyBytes、readTimeout 均可调整
3. **零依赖**：仅使用 bufio + strconv，不依赖 net/http 内部 API

代价是不支持 Keep-Alive 和 HTTP/2，这是当前版本的已知限制，将在零拷贝代理阶段解决。
