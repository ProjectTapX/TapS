package instance

import (
	"bytes"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// pickEncoder returns the encoding declared in cfg.OutputEncoding, or nil
// if the daemon should pass bytes through unchanged.
func pickEncoder(name string) encoding.Encoding {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "utf-8", "utf8":
		return nil
	case "gbk", "cp936", "windows-936":
		return simplifiedchinese.GBK
	case "gb18030":
		return simplifiedchinese.GB18030
	case "big5":
		return traditionalchinese.Big5
	}
	return nil
}

// decodeToUTF8 converts data from the configured encoding into UTF-8.
// Errors fall back to the raw bytes — better to show garbled than nothing.
func decodeToUTF8(name string, data []byte) string {
	enc := pickEncoder(name)
	if enc == nil {
		return string(data)
	}
	out, _, err := transform.Bytes(enc.NewDecoder(), data)
	if err != nil {
		return string(data)
	}
	return string(out)
}

// encodeFromUTF8 converts user input (UTF-8) into the process's expected
// encoding before writing to its stdin.
func encodeFromUTF8(name, data string) []byte {
	enc := pickEncoder(name)
	if enc == nil {
		return []byte(data)
	}
	out, _, err := transform.Bytes(enc.NewEncoder(), []byte(data))
	if err != nil {
		return []byte(data)
	}
	return out
}

var _ = bytes.NewReader // keep import in case future buffered conversion is added
