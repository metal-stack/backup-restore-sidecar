package encryption

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncrypter(t *testing.T) {
	// Key too short
	_, err := New(slog.Default(), "tooshortkey")
	assert.EqualError(t, err, "key length:11 invalid, must be 16,24 or 32 bytes")

	// Key too short
	_, err = New(slog.Default(), "19bytesofencryption")
	assert.EqualError(t, err, "key length:19 invalid, must be 16,24 or 32 bytes")

	// Key too long
	_, err = New(slog.Default(), "tooloooonoooooooooooooooooooooooooooongkey")
	assert.EqualError(t, err, "key length:42 invalid, must be 16,24 or 32 bytes")

	e, err := New(slog.Default(), "01234567891234560123456789123456")
	assert.NoError(t, err, "")

	input, err := os.CreateTemp("", "encrypt")
	assert.NoError(t, err)
	defer os.Remove(input.Name())

	cleartextInput := []byte("This is the content of the file")
	err = os.WriteFile(input.Name(), cleartextInput, 0644)
	assert.NoError(t, err)
	output, err := e.Encrypt(input.Name())
	assert.NoError(t, err)

	assert.Equal(t, input.Name()+Suffix, output)

	cleartextFile, err := e.Decrypt(output)
	assert.NoError(t, err)
	cleartext, err := os.ReadFile(cleartextFile)
	assert.NoError(t, err)
	assert.Equal(t, cleartextInput, cleartext)

	// Test with 100MB file
	bigBuff := make([]byte, 100000000)
	err = os.WriteFile("bigfile.test", bigBuff, 0666)
	assert.NoError(t, err)

	bigEncFile, err := e.Encrypt("bigfile.test")
	assert.NoError(t, err)
	_, err = e.Decrypt(bigEncFile)
	assert.NoError(t, err)
	os.Remove("bigfile.test")
	os.Remove("bigfile.test.aes")
}
