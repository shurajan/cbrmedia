package hls

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
)

// decryptAES128 расшифровывает сегмент AES-128-CBC с PKCS#7-паддингом на месте.
func decryptAES128(data, key, iv []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, fmt.Errorf("invalid AES-128 key length %d", len(key))
	}
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("invalid IV length %d", len(iv))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(data)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d not a multiple of block size", len(data))
	}
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(data, data)
	return pkcs7Unpad(data)
}

// seqToIV выводит IV для AES-128 из номера сегмента, если плейлист не задаёт его
// явно (big-endian, по спецификации HLS).
func seqToIV(seq int) []byte {
	iv := make([]byte, aes.BlockSize)
	if seq < 0 { // номер сегмента в HLS неотрицательный; защита от мусора в плейлисте
		seq = 0
	}
	binary.BigEndian.PutUint64(iv[8:], uint64(seq))
	return iv
}

func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 {
		return b, nil
	}
	pad := int(b[len(b)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(b) {
		return nil, errors.New("invalid PKCS7 padding")
	}
	if !bytes.Equal(b[len(b)-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return nil, errors.New("invalid PKCS7 padding bytes")
	}
	return b[:len(b)-pad], nil
}
