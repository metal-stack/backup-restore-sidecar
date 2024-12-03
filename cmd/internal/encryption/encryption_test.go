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

	//Prepare input for encryption
	input, err := fs.Create("encrypt")
	require.NoError(t, err)
	output, err := fs.Create("encrypt.aes")
	require.NoError(t, err)
	cleartextInput := []byte("This is the content of the file")
	err = afero.WriteFile(fs, input.Name(), cleartextInput, 0600)
	require.NoError(t, err)

	//Encrypt files
	err = e.Encrypt(input, output)
	require.NoError(t, err)
	encryptedText, err := afero.ReadFile(fs, output.Name())
	require.NoError(t, err)

	require.Equal(t, input.Name()+Suffix, output.Name())
	require.NotEqual(t, cleartextInput, encryptedText)

	//Prepare input for decryption
	inputDecrypted, err := fs.Open("encrypt.aes")
	require.NoError(t, err)
	outputDecrypted, err := fs.Create("decrypted")
	require.NoError(t, err)

	//Decrypt files
	err = e.Decrypt(inputDecrypted, outputDecrypted)
	require.NoError(t, err)
	cleartext, err := afero.ReadFile(fs, outputDecrypted.Name())

	require.NoError(t, err)
	require.Equal(t, cleartextInput, cleartext)

	// Test with 100MB file
	bigBuff := make([]byte, 100000000)
	err = afero.WriteFile(fs, "bigfile.test", bigBuff, 0600)
	require.NoError(t, err)

	inputBigEnc, err := fs.Open("bigfile.test")
	require.NoError(t, err)
	outputBigEnc, err := fs.Create("encrypted_big.test.aes")
	require.NoError(t, err)

	err = e.Encrypt(inputBigEnc, outputBigEnc)
	require.NoError(t, err)

	inputBigDec, err := fs.Open("encrypted_big.test.aes")
	require.NoError(t, err)
	outputBigDec, err := fs.Create("decrypted_big.test.aes")
	require.NoError(t, err)
	err = e.Decrypt(inputBigDec, outputBigDec)
	require.NoError(t, err)

	err = fs.Remove(input.Name())
	require.NoError(t, err)
	err = fs.Remove(output.Name())
	require.NoError(t, err)
	err = fs.Remove(outputDecrypted.Name())
	require.NoError(t, err)
	err = fs.Remove(inputBigEnc.Name())
	require.NoError(t, err)
	err = fs.Remove(outputBigEnc.Name())
	require.NoError(t, err)
	err = fs.Remove(outputBigDec.Name())
	require.NoError(t, err)
}
