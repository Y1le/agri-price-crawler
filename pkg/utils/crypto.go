package utils

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha512"
	"fmt"
	"math/big"
	"strings"
)

// md5 hex
func Md5Encrypt(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

// sha1 hex
func Sha1Encrypt(s string) string {
	h := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

// sha384 hex
func Sha384Encrypt(s string) string {
	h := sha512.New384()
	h.Write([]byte(s))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// 将 hex 字符串转为十进制字符串（模拟 Long.fromString(hex, true, 16).toString(10)）
func HexToDecimal(hexStr string) string {
	if hexStr == "" {
		return "0"
	}
	n := new(big.Int)
	n.SetString(hexStr, 16)
	return n.String()
}

func Base36EncodeFixed(num int64, width int) string {
	const base36Chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	if num == 0 {
		return strings.Repeat("0", width)
	}
	s := ""
	n := num
	for n > 0 {
		s = string(base36Chars[n%36]) + s
		n /= 36
	}
	if len(s) >= width {
		return s[:width]
	}
	return strings.Repeat("0", width-len(s)) + s
}
