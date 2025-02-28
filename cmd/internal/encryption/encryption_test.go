package encryption

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"testing"

	"github.com/metal-stack/backup-restore-sidecar/cmd/internal/backup/providers/local"
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

	//prepare input for encryption
	input, err := fs.Create("encrypt")
	require.NoError(t, err)
	output, err := fs.Create("encrypt.aes")
	require.NoError(t, err)
	cleartextInput := []byte("This is the content of the file")
	err = afero.WriteFile(fs, input.Name(), cleartextInput, 0600)
	require.NoError(t, err)

	err = e.Encrypt(input, output)
	require.NoError(t, err)
	encryptedText, err := afero.ReadFile(fs, output.Name())
	require.NoError(t, err)

	require.Equal(t, input.Name()+suffix, output.Name())
	require.NotEqual(t, cleartextInput, encryptedText)

	//prepare input for decryption
	inputDecrypted, err := fs.Open("encrypt.aes")
	require.NoError(t, err)
	outputDecrypted, err := fs.Create("decrypt")
	require.NoError(t, err)

	err = e.Decrypt(inputDecrypted, outputDecrypted)
	require.NoError(t, err)
	cleartext, err := afero.ReadFile(fs, outputDecrypted.Name())
	require.NoError(t, err)
	require.Equal(t, cleartextInput, cleartext)

	// Test with 1GB file
	bigBuff := make([]byte, 1000000000)
	err = afero.WriteFile(fs, "bigfile", bigBuff, 0600)
	require.NoError(t, err)

	inputBigEnc, err := fs.Create("bigfile")
	require.NoError(t, err)
	outputBigEnc, err := fs.Create("bigfile.aes")
	require.NoError(t, err)
	err = e.Encrypt(inputBigEnc, outputBigEnc)
	require.NoError(t, err)

	inputBigDec, err := fs.Open("bigfile.aes")
	require.NoError(t, err)
	outputBigDec, err := fs.Create("bigfile-decrypted")
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

func TestNonBlockingEncryption(t *testing.T) {
	fs := afero.NewMemMapFs()
	err := fs.Mkdir("/backup", 0777)
	require.NoError(t, err)
	bp, err := local.New(
		slog.Default(),
		&local.BackupProviderConfigLocal{
			LocalBackupPath: "/backup",
			ObjectsToKeep:   10,
			Suffix:          suffix,
			FS:              fs,
		},
	)
	require.NoError(t, err, "")
	e, err := New(slog.Default(), &EncrypterConfig{Key: "01234567891234560123456789123456", FS: fs})
	require.NoError(t, err, "")

	// Test with 100MB file
	bigBuff := make([]byte, 100000000)
	err = afero.WriteFile(fs, "bigfile", bigBuff, 0600)
	require.NoError(t, err)

	inputBigEnc, err := fs.Open("bigfile")
	require.NoError(t, err)
	outputBigEnc, err := fs.Create("bigfile.aes")
	require.NoError(t, err)

	pr, pw := io.Pipe()

	encryptErr := make(chan error, 1)
	uploadErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer pw.Close()
		defer close(encryptErr)
		defer wg.Done()

		fmt.Println("🔒 Starting encryption")
		encryptErr <- e.Encrypt(inputBigEnc, pw)
		fmt.Println("🔒 Ending encryption")
	}()
	go func() {
		defer close(uploadErr)
		defer wg.Done()

		fmt.Println("☁️ Starting upload")
		uploadErr <- bp.UploadBackup(context.Background(), pr)
		fmt.Println("☁️ Ending upload")
	}()

	wg.Wait()

	select {
	case err = <-encryptErr:
		require.NoError(t, err)
	case err = <-uploadErr:
		require.NoError(t, err)
	}

	err = fs.Remove(inputBigEnc.Name())
	require.NoError(t, err)
	err = fs.Remove(outputBigEnc.Name())
	require.NoError(t, err)
}

func BenchmarkEncrypter(b *testing.B) {
	fs := afero.NewMemMapFs()

	e, err := New(slog.Default(), &EncrypterConfig{Key: "01234567891234560123456789123456", FS: fs})
	require.NoError(b, err, "")

	// Test with 1GB file
	bigBuff := make([]byte, 1000000000)
	err = afero.WriteFile(fs, "bigfile", bigBuff, 0600)
	require.NoError(b, err)

	inputBigEnc, err := fs.Create("bigfile")
	require.NoError(b, err)
	outputBigEnc, err := fs.Create("bigfile.aes")
	require.NoError(b, err)

	b.ReportAllocs()
	b.ResetTimer()
	var mem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&mem)
	fmt.Println("Before encryption: TotalAlloc", mem.TotalAlloc, "HeapAlloc:", mem.HeapAlloc)
	err = e.Encrypt(inputBigEnc, outputBigEnc)
	runtime.ReadMemStats(&mem)
	fmt.Println("After encryption: TotalAlloc", mem.TotalAlloc, "HeapAlloc:", mem.HeapAlloc)

	require.NoError(b, err)
	b.StopTimer()

	require.NoError(b, err)
	err = fs.Remove(inputBigEnc.Name())
	require.NoError(b, err)
	err = fs.Remove(outputBigEnc.Name())
	require.NoError(b, err)
}
