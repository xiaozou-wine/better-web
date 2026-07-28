package geo

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// readAtMost 最多读取 limit 字节。超出上限视为错误而非静默截断，
// 因为截断的 JSON 会在解析阶段报出难以定位的错误。
func readAtMost(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("响应体超过 %d 字节上限", limit)
	}
	return b, nil
}

// parseCloudflareTrace 解析 /cdn-cgi/trace 的 key=value 行格式。
// 该接口只给国家码，不给地区码，因此美国出口会落到东部默认时区。
func parseCloudflareTrace(b []byte) (country, region string, err error) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		if key == "loc" {
			return strings.TrimSpace(val), "", nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", "", err
	}
	return "", "", fmt.Errorf("trace 响应中缺少 loc 字段")
}
