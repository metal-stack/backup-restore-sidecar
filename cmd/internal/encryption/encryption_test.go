package encryption

import (
	"log/slog"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestEncrypter(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Key too short
	_, err := New(slog.Default(), &EncrypterConfig{Key: "tooshortkey", FS: fs})
	require.EqualError(t, err, "key length: 11 invalid, must be 32 bytes")

	// Key too long
	_, err = New(slog.Default(), &EncrypterConfig{Key: "toolooooooooooooooooooooooooooooooooongkey", FS: fs})
	require.EqualError(t, err, "key length: 42 invalid, must be 32 bytes")

	_, err = New(slog.Default(), &EncrypterConfig{Key: "äöüäöüäöüäöüäöüä", FS: fs})
	require.EqualError(t, err, "key must only contain ascii characters")

	e, err := New(slog.Default(), &EncrypterConfig{Key: "01234567891234560123456789123456", FS: fs})
	require.NoError(t, err, "")

	input, err := fs.Create("encrypt")
	require.NoError(t, err)

	cleartextInput := []byte("This is the content of the file")
	err = afero.WriteFile(fs, input.Name(), cleartextInput, 0600)
	require.NoError(t, err)
	output, err := e.Encrypt(input.Name())
	require.NoError(t, err)
	encryptedText, err := afero.ReadFile(fs, output)
	require.NoError(t, err)

	require.Equal(t, input.Name()+suffix, output)
	require.NotEqual(t, cleartextInput, encryptedText)

	cleartextFile, err := e.Decrypt(output)
	require.NoError(t, err)
	cleartext, err := afero.ReadFile(fs, cleartextFile)
	require.NoError(t, err)
	require.Equal(t, cleartextInput, cleartext)

	// Test with 100MB file
	bigBuff := make([]byte, 100000000)
	err = afero.WriteFile(fs, "bigfile.test", bigBuff, 0600)
	require.NoError(t, err)

	bigEncFile, err := e.Encrypt("bigfile.test")
	require.NoError(t, err)
	_, err = e.Decrypt(bigEncFile)
	require.NoError(t, err)

	err = fs.Remove(input.Name())
	require.NoError(t, err)
	err = fs.Remove("bigfile.test")
	require.NoError(t, err)
}
