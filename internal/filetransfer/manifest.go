package filetransfer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

const DefaultChunkSize int64 = 8 * 1024 * 1024

var ErrInvalidChunkSize = errors.New("chunk size must be positive")

type Chunk struct {
	Index  int      `json:"index"`
	Size   int      `json:"size"`
	SHA256 [32]byte `json:"sha256"`
}

type Manifest struct {
	Size      int64    `json:"size"`
	SHA256    [32]byte `json:"sha256"`
	ChunkSize int64    `json:"chunkSize"`
	Chunks    []Chunk  `json:"chunks"`
}

func BuildManifest(reader io.Reader, chunkSize int64) (Manifest, error) {
	if chunkSize <= 0 {
		return Manifest{}, ErrInvalidChunkSize
	}
	if chunkSize > int64(int(^uint(0)>>1)) {
		return Manifest{}, fmt.Errorf("%w: exceeds platform capacity", ErrInvalidChunkSize)
	}

	manifest := Manifest{ChunkSize: chunkSize}
	wholeFileHash := sha256.New()
	buffer := make([]byte, int(chunkSize))
	for index := 0; ; index++ {
		read, err := io.ReadFull(reader, buffer)
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return Manifest{}, fmt.Errorf("read chunk %d: %w", index, err)
		}
		if read > 0 {
			data := buffer[:read]
			_, _ = wholeFileHash.Write(data)
			manifest.Chunks = append(manifest.Chunks, Chunk{
				Index:  index,
				Size:   read,
				SHA256: sha256.Sum256(data),
			})
			manifest.Size += int64(read)
		}
		if err != nil {
			break
		}
	}
	copy(manifest.SHA256[:], wholeFileHash.Sum(nil))
	return manifest, nil
}

func (manifest Manifest) VerifyChunk(index int, data []byte) bool {
	if index < 0 || index >= len(manifest.Chunks) {
		return false
	}
	chunk := manifest.Chunks[index]
	return chunk.Index == index && chunk.Size == len(data) && chunk.SHA256 == sha256.Sum256(data)
}

func (manifest Manifest) VerifyFile(reader io.Reader) (bool, error) {
	hash := sha256.New()
	size, err := io.Copy(hash, reader)
	if err != nil {
		return false, fmt.Errorf("hash file: %w", err)
	}
	if size != manifest.Size {
		return false, nil
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest == manifest.SHA256, nil
}
