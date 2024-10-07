package encryption

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncrypter(t *testing.T) {
	// Key too short
	_, err := New(slog.Default(), "tooshortkey")
	require.EqualError(t, err, "key length:11 invalid, must be 16,24 or 32 bytes")

	// Key too short
	_, err = New(slog.Default(), "19bytesofencryption")
	require.EqualError(t, err, "key length:19 invalid, must be 16,24 or 32 bytes")

	// Key too long
	_, err = New(slog.Default(), "tooloooonoooooooooooooooooooooooooooongkey")
	require.EqualError(t, err, "key length:42 invalid, must be 16,24 or 32 bytes")

	e, err := New(slog.Default(), "01234567891234560123456789123456")
	require.NoError(t, err, "")

	input, err := os.CreateTemp("", "encrypt")
	require.NoError(t, err)
	defer os.Remove(input.Name())

	cleartextInput := []byte("This is the content of the file")
	err = os.WriteFile(input.Name(), cleartextInput, 0600)
	require.NoError(t, err)
	output, err := e.Encrypt(input.Name())
	require.NoError(t, err)

	require.Equal(t, input.Name()+Suffix, output)

	cleartextFile, err := e.Decrypt(output)
	require.NoError(t, err)
	cleartext, err := os.ReadFile(cleartextFile)
	require.NoError(t, err)
	require.Equal(t, cleartextInput, cleartext)

	// Test with 100MB file
	bigBuff := make([]byte, 100000000)
	err = os.WriteFile("bigfile.test", bigBuff, 0600)
	require.NoError(t, err)

	bigEncFile, err := e.Encrypt("bigfile.test")
	require.NoError(t, err)
	_, err = e.Decrypt(bigEncFile)
	require.NoError(t, err)
	os.Remove("bigfile.test")
	os.Remove("bigfile.test.aes")
}
