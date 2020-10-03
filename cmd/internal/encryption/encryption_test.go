package encryption

import (
	"io/ioutil"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestEncrypter(t *testing.T) {
	// Key too short
	e, err := New(zap.L().Sugar(), "tooshortkey")
	assert.EqualError(t, err, "key length:11 invalid, must be 16,24 or 32 bytes")

	// Key too short
	e, err = New(zap.L().Sugar(), "19bytesofencryption")
	assert.EqualError(t, err, "key length:19 invalid, must be 16,24 or 32 bytes")

	// Key too long
	e, err = New(zap.L().Sugar(), "tooloooonoooooooooooooooooooooooooooongkey")
	assert.EqualError(t, err, "key length:42 invalid, must be 16,24 or 32 bytes")

	e, err = New(zap.L().Sugar(), "0123456789123456")
	assert.NoError(t, err, "")

	input, err := ioutil.TempFile("", "encrypt")
	cleartextInput := []byte("This is the content of the file")
	err = ioutil.WriteFile(input.Name(), cleartextInput, 0644)
	assert.NoError(t, err)
	output, err := e.Encrypt(input.Name())
	assert.NoError(t, err)

	assert.Equal(t, input.Name()+Suffix, output)

	cleartextFile, err := e.Decrypt(output)
	assert.NoError(t, err)
	cleartext, err := ioutil.ReadFile(cleartextFile)
	assert.NoError(t, err)
	assert.Equal(t, cleartextInput, cleartext)

}
