package compress

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCompress(t *testing.T) {
	ctx := context.Background()
	fsys := afero.NewMemMapFs()

	compressor, err := New(&CompressorConfig{Method: "targz", FS: fsys})
	require.NoError(t, err)

	// Prepare input for compression
	input, err := fsys.Create("compress")
	require.NoError(t, err)

	err = afero.WriteFile(fsys, input.Name(), []byte("This is the content of the file"), 0600)
	require.NoError(t, err)

	// Compress files
	output, err := fsys.Create("compress.targz")
	require.NoError(t, err)

	files, err := compressor.BuildFilesForCompression("compress", input.Name())
	require.NoError(t, err)

	err = compressor.Compress(ctx, output, files)
	require.NoError(t, err)

	// Prepare input for decompression
	inputDecompressed, err := fsys.Open("compress.targz")
	require.NoError(t, err)

	err = fsys.Remove("compress")
	require.NoError(t, err)

	// Decompress files
	err = compressor.Decompress(ctx, inputDecompressed, "./")
	require.NoError(t, err)

	outputDecompressed, err := fsys.Open("compress")
	require.NoError(t, err)

	cleartext, err := afero.ReadFile(fsys, outputDecompressed.Name())
	require.NoError(t, err)

	println("cleartext: ")
	println(string(cleartext))

	require.Equal(t, []byte("This is the content of the file"), (cleartext))

}
