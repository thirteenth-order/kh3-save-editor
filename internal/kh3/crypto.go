// Package kh3 implements the Kingdom Hearts III (PC) save container.
package kh3

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
)

const (
	TrailerLen = 17
	TrailerSep = 0x08
	BlockSize  = 16
)

var Magic = []byte("S@vE")

var (
	keyMask = []byte("hN96q4X9f%BCURBV&pMT4kcvqTMhHYD&")
	keyIdx  = []byte("ABCDE!#$%&FGHIJ012345KLMNOPqrstuvwxyzQRSTUVWXYZ" +
		"6789abcdefgh},.<>ijklmnop()=~|-^+*;:[]{/?_@")
)

// DeriveKey builds the 32-byte AES key from an account id (SteamID64).
func DeriveKey(account string) ([]byte, error) {
	if account == "" {
		return nil, errors.New("account id must not be empty")
	}
	acc := []byte(account)
	key := make([]byte, 32)
	j := 1
	for i := 0; i < 32; i++ {
		key[i] = keyIdx[(keyMask[i]^acc[j%len(acc)])%0x5A]
		j++
	}
	return key, nil
}

// ecb runs every 16-byte block through f. There is no IV and no chaining,
// which is exactly why this format is breakable block-for-block.
func ecb(dst, src []byte, f func(dst, src []byte)) {
	for i := 0; i+BlockSize <= len(src); i += BlockSize {
		f(dst[i:i+BlockSize], src[i:i+BlockSize])
	}
}

func fileSize(plain []byte) uint32 { return binary.LittleEndian.Uint32(plain[4:8]) }

// decryptHead decrypts a single block, for the cheap key oracle.
func decryptHead(block16, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, BlockSize)
	block.Decrypt(out, block16)
	return out, nil
}

// Unwrap decrypts an on-disk save and validates both integrity fields.
func Unwrap(blob, key []byte) ([]byte, error) {
	if len(blob) <= TrailerLen {
		return nil, fmt.Errorf("file too short (%d bytes)", len(blob))
	}
	body := blob[:len(blob)-TrailerLen]
	if len(body)%BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d is not a multiple of the AES block size", len(body))
	}
	if sep := blob[len(blob)-TrailerLen]; sep != TrailerSep {
		return nil, fmt.Errorf("unexpected trailer separator 0x%02x (want 0x08)", sep)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(body))
	ecb(plain, body, block.Decrypt)
	if !bytes.Equal(plain[:4], Magic) {
		return nil, errors.New("decrypted data does not start with 'S@vE'; wrong account id or key")
	}
	fs := fileSize(plain)
	if fs == 0 || int(fs)+24 > len(plain) {
		return nil, fmt.Errorf("implausible filesize field 0x%x", fs)
	}
	sum := md5.Sum(plain[:fs+16])
	if !bytes.Equal(sum[:], blob[len(blob)-16:]) {
		return nil, errors.New("trailing MD5 mismatch; file is corrupt or truncated")
	}
	return plain, nil
}

// Wrap rebuilds both integrity fields and encrypts back to on-disk form.
func Wrap(plain, key []byte) ([]byte, error) {
	if !bytes.Equal(plain[:4], Magic) {
		return nil, errors.New("plaintext does not start with 'S@vE'")
	}
	if len(plain)%BlockSize != 0 {
		return nil, fmt.Errorf("plaintext length %d is not block aligned", len(plain))
	}
	out := make([]byte, len(plain))
	copy(out, plain)
	fs := fileSize(out)

	binary.LittleEndian.PutUint32(out[0x0C:0x10], crc32.ChecksumIEEE(out[0x10:0x10+fs]))
	sum := md5.Sum(out[:fs+16])

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	blob := make([]byte, len(out)+TrailerLen)
	ecb(blob, out, block.Encrypt)
	blob[len(out)] = TrailerSep
	copy(blob[len(out)+1:], sum[:])
	return blob, nil
}
