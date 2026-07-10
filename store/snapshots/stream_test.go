package snapshots

import (
	"bytes"
	"compress/zlib"
	"io"
	"testing"

	protoio "github.com/cosmos/gogoproto/io"
	"github.com/stretchr/testify/require"

	snapshottypes "github.com/cosmos/cosmos-sdk/store/v2/snapshots/types"
)

func TestChunkBoundedReader(t *testing.T) {
	ch := make(chan io.ReadCloser, 2)
	ch <- io.NopCloser(bytes.NewReader([]byte{1}))
	ch <- io.NopCloser(bytes.NewReader([]byte{2}))
	close(ch)
	chunkReader := NewChunkReader(ch)

	src := bytes.NewReader(bytes.Repeat([]byte{0xAA}, 100))
	bounded := newChunkBoundedReader(src, chunkReader, 4)

	buf := make([]byte, 4)
	n, err := bounded.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	// budget exhausted, no boundary crossed yet
	_, err = bounded.Read(buf)
	require.ErrorIs(t, err, snapshottypes.ErrDecompressedChunkTooLarge)
	_, err = bounded.Read(buf)
	require.ErrorIs(t, err, snapshottypes.ErrDecompressedChunkTooLarge)

	// cross into the second physical chunk
	one := make([]byte, 1)
	_, err = chunkReader.Read(one)
	require.NoError(t, err)
	require.Equal(t, 1, chunkReader.chunksOpened)
	_, err = chunkReader.Read(one)
	require.NoError(t, err)
	require.Equal(t, 2, chunkReader.chunksOpened)

	// new chunk grants a fresh budget
	n, err = bounded.Read(buf)
	require.NoError(t, err)
	require.Equal(t, 4, n)
}

func TestStreamReaderDecompressedChunkLimit(t *testing.T) {
	const payloadSize = 64 * 1024
	item := &snapshottypes.SnapshotItem{
		Item: &snapshottypes.SnapshotItem_ExtensionPayload{
			ExtensionPayload: &snapshottypes.SnapshotExtensionPayload{Payload: bytes.Repeat([]byte{0xAA}, payloadSize)},
		},
	}

	// A highly compressible item fits in one small physical chunk.
	var compressed bytes.Buffer
	zWriter := zlib.NewWriter(&compressed)
	require.NoError(t, protoio.NewDelimitedWriter(zWriter).WriteMsg(item))
	require.NoError(t, zWriter.Close())
	require.Less(t, compressed.Len(), payloadSize/10)

	testCases := []struct {
		name     string
		maxBytes int64
		expErr   error
	}{
		{"decompressed size within limit", 2 * payloadSize, nil},
		{"decompressed size exceeds limit", payloadSize / 2, snapshottypes.ErrDecompressedChunkTooLarge},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan io.ReadCloser, 1)
			ch <- io.NopCloser(bytes.NewReader(compressed.Bytes()))
			close(ch)

			reader, err := newStreamReader(ch, tc.maxBytes)
			require.NoError(t, err)
			defer reader.Close()

			var got snapshottypes.SnapshotItem
			err = reader.ReadMsg(&got)
			if tc.expErr != nil {
				require.ErrorIs(t, err, tc.expErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, item.GetExtensionPayload().Payload, got.GetExtensionPayload().Payload)
		})
	}
}
