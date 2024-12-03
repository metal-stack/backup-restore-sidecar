package compress

import (
	"context"
	"io/fs"
	"testing"

	"github.com/metal-stack/backup-restore-sidecar/pkg/constants"
	"github.com/mholt/archives"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestCompress(t *testing.T) {
	ctx := context.Background()
	fsys := afero.NewMemMapFs()

	compressor, err := New(&CompressorConfig{Method: "targz", FS: fsys})
	require.NoError(t, err)

	// Prepare input for compression
	input, err := fsys.Create(constants.BackupDir + "/compress.db")
	require.NoError(t, err)

	err = afero.WriteFile(fsys, input.Name(), []byte("This is the content of the file"), 0600)
	require.NoError(t, err)

	// Compress files
	output, err := fsys.Create(constants.RestoreDir + "/compress.targz")
	require.NoError(t, err)

	//Need to mock fileInfo, since `FilesFromDisk` use os-filesystem
	files := []archives.FileInfo{}
	backupDir, err := fsys.Open(constants.BackupDir)
	require.NoError(t, err)

	dirStat, err := backupDir.Stat()
	require.NoError(t, err)
	backupStat, err := input.Stat()
	require.NoError(t, err)

	files = append(files, archives.FileInfo{Header: ".targz", FileInfo: dirStat, NameInArchive: "compress.targz", LinkTarget: "", Open: func() (fs.File, error) {
		return fsys.Open(constants.BackupDir)
	}})
	files = append(files, archives.FileInfo{Header: ".targz", FileInfo: backupStat, NameInArchive: "compress.db", LinkTarget: "", Open: func() (fs.File, error) {
		return fsys.Open(constants.BackupDir + "/compress.db")
	}})

	// Compress files
	err = compressor.Compress(ctx, output, files)
	require.NoError(t, err)

	// Prepare input for decompression
	inputDecompressed, err := fsys.Open(constants.RestoreDir + "/compress.targz")
	require.NoError(t, err)

	// Decompress files
	err = compressor.Decompress(ctx, inputDecompressed)
	require.NoError(t, err)

	outputDecompressed, err := fsys.Open(constants.RestoreDir + "/compress.db")
	require.NoError(t, err)

	cleartext, err := afero.ReadFile(fsys, outputDecompressed.Name())
	require.NoError(t, err)

	println("cleartext: ")
	println(string(cleartext))

	require.Equal(t, []byte("This is the content of the file"), (cleartext))

}
